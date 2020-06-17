package server

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/gogo/protobuf/types"
	opentracing "github.com/opentracing/opentracing-go"
	"github.com/robfig/cron"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kube_watch "k8s.io/apimachinery/pkg/watch"

	"github.com/pachyderm/pachyderm/src/client"
	"github.com/pachyderm/pachyderm/src/client/pfs"
	"github.com/pachyderm/pachyderm/src/client/pkg/errors"
	"github.com/pachyderm/pachyderm/src/client/pkg/tracing"
	"github.com/pachyderm/pachyderm/src/client/pkg/tracing/extended"
	"github.com/pachyderm/pachyderm/src/client/pps"
	pfsServer "github.com/pachyderm/pachyderm/src/server/pfs"
	"github.com/pachyderm/pachyderm/src/server/pkg/backoff"
	"github.com/pachyderm/pachyderm/src/server/pkg/dlock"
	"github.com/pachyderm/pachyderm/src/server/pkg/ppsconsts"
	"github.com/pachyderm/pachyderm/src/server/pkg/ppsutil"
	"github.com/pachyderm/pachyderm/src/server/pkg/watch"
)

const (
	masterLockPath = "_master_lock"
)

var (
	failures = map[string]bool{
		"InvalidImageName": true,
		"ErrImagePull":     true,
		"Unschedulable":    true,
	}

	zero     int32 // used to turn down RCs in scaleDownWorkersForPipeline
	falseVal bool  // used to delete RCs in deletePipelineResources and restartPipeline()
)

// The master process is responsible for creating/deleting workers as
// pipelines are created/removed.
func (a *apiServer) master() {
	masterLock := dlock.NewDLock(a.env.GetEtcdClient(), path.Join(a.etcdPrefix, masterLockPath))
	backoff.RetryNotify(func() error {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		// Note: 'pachClient' is unauthenticated. This will use the PPS token (via
		// a.sudo()) to authenticate requests.
		pachClient := a.env.GetPachClient(ctx)
		ctx, err := masterLock.Lock(ctx)
		if err != nil {
			return err
		}
		defer masterLock.Unlock(ctx)
		kubeClient := a.env.GetKubeClient()

		log.Infof("PPS master: launching master process")

		// TODO(msteffen) request only keys, since pipeline_controller.go reads
		// fresh values for each event anyway
		pipelineWatcher, err := a.pipelines.ReadOnly(ctx).Watch()
		if err != nil {
			return errors.Wrapf(err, "error creating watch")
		}
		defer pipelineWatcher.Close()

		// watchChan will be nil if the Watch call below errors, this means
		// that we won't receive events from k8s and won't be able to detect
		// errors in pods. We could just return that error and retry but that
		// prevents pachyderm from creating pipelines when there's an issue
		// talking to k8s.
		var watchChan <-chan kube_watch.Event
		kubePipelineWatch, err := kubeClient.CoreV1().Pods(a.namespace).Watch(
			metav1.ListOptions{
				LabelSelector: metav1.FormatLabelSelector(metav1.SetAsLabelSelector(
					map[string]string{
						"component": "worker",
					})),
				Watch: true,
			})
		if err != nil {
			log.Errorf("failed to watch kuburnetes pods: %v", err)
		} else {
			watchChan = kubePipelineWatch.ResultChan()
			defer kubePipelineWatch.Stop()
		}

		for {
			select {
			case event := <-pipelineWatcher.Watch():
				if event.Err != nil {
					return errors.Wrapf(event.Err, "event err")
				}
				switch event.Type {
				case watch.EventPut:
					pipeline := string(event.Key)
					// Create/Modify/Delete pipeline resources as needed per new state
					if err := a.step(pachClient, pipeline, event.Ver, event.Rev); err != nil {
						log.Errorf("PPS master: %v", err)
					}
				}
			case event := <-watchChan:
				// if we get an error we restart the watch, k8s watches seem to
				// sometimes get stuck in a loop returning events with Type =
				// "" we treat these as errors since otherwise we get an
				// endless stream of them and can't do anything.
				if event.Type == kube_watch.Error || event.Type == "" {
					if kubePipelineWatch != nil {
						kubePipelineWatch.Stop()
					}
					kubePipelineWatch, err = kubeClient.CoreV1().Pods(a.namespace).Watch(
						metav1.ListOptions{
							LabelSelector: metav1.FormatLabelSelector(metav1.SetAsLabelSelector(
								map[string]string{
									"component": "worker",
								})),
							Watch: true,
						})
					if err != nil {
						log.Errorf("failed to watch kuburnetes pods: %v", err)
						watchChan = nil
					} else {
						watchChan = kubePipelineWatch.ResultChan()
						defer kubePipelineWatch.Stop()
					}
				}
				pod, ok := event.Object.(*v1.Pod)
				if !ok {
					continue
				}
				if pod.Status.Phase == v1.PodFailed {
					log.Errorf("pod failed because: %s", pod.Status.Message)
				}
				pipelineName := pod.ObjectMeta.Annotations["pipelineName"]
				for _, status := range pod.Status.ContainerStatuses {
					if status.State.Waiting != nil && failures[status.State.Waiting.Reason] {
						if err := a.setPipelineCrashing(pachClient.Ctx(), pipelineName, status.State.Waiting.Message); err != nil {
							return err
						}
					}
				}
				for _, condition := range pod.Status.Conditions {
					if condition.Type == v1.PodScheduled &&
						condition.Status != v1.ConditionTrue && failures[condition.Reason] {
						if err := a.setPipelineCrashing(pachClient.Ctx(), pipelineName, condition.Message); err != nil {
							return err
						}
					}
				}
			}
		}
	}, backoff.NewInfiniteBackOff(), func(err error, d time.Duration) error {
		// cancel all monitorPipeline goroutines
		a.monitorCancelsMu.Lock()
		defer a.monitorCancelsMu.Unlock()
		for _, c := range a.monitorCancels {
			c()
		}
		a.monitorCancels = make(map[string]func())
		log.Errorf("PPS master: error running the master process: %v; retrying in %v", err, d)
		return nil
	})
	panic("internal error: PPS master has somehow exited. Restarting pod...")
}

func (a *apiServer) setPipelineFailure(ctx context.Context, pipelineName string, reason string) error {
	return a.setPipelineState(ctx, pipelineName, pps.PipelineState_PIPELINE_FAILURE, reason)
}

func (a *apiServer) setPipelineCrashing(ctx context.Context, pipelineName string, reason string) error {
	return a.setPipelineState(ctx, pipelineName, pps.PipelineState_PIPELINE_CRASHING, reason)
}

// Every running pipeline with standby == true has a corresponding goroutine
// running monitorPipeline() that puts the pipeline in and out of standby in
// response to new output commits appearing in that pipeline's output repo
func (a *apiServer) cancelMonitor(pipeline string) {
	a.monitorCancelsMu.Lock()
	defer a.monitorCancelsMu.Unlock()
	if cancel, ok := a.monitorCancels[pipeline]; ok {
		cancel()
		delete(a.monitorCancels, pipeline)
	}
}

// Every crashing pipeline has a corresponding goro running
// monitorCrashingPipeline that checks to see if the issues have resolved
// themselves and moves the pipeline out of crashing if they have.
func (a *apiServer) cancelCrashingMonitor(pipeline string) {
	a.monitorCancelsMu.Lock()
	defer a.monitorCancelsMu.Unlock()
	if cancel, ok := a.crashingMonitorCancels[pipeline]; ok {
		cancel()
		delete(a.crashingMonitorCancels, pipeline)
	}
}

func (a *apiServer) deletePipelineResources(ctx context.Context, pipelineName string) (retErr error) {
	log.Infof("PPS master: deleting resources for pipeline %q", pipelineName)
	span, ctx := tracing.AddSpanToAnyExisting(ctx, //lint:ignore SA4006 ctx is unused, but better to have the right ctx in scope so people don't use the wrong one
		"/pps.Master/DeletePipelineResources", "pipeline", pipelineName)
	defer func() {
		tracing.TagAnySpan(span, "err", retErr)
		tracing.FinishAnySpan(span)
	}()

	// Cancel any running monitorPipeline call
	a.cancelMonitor(pipelineName)
	// Same for cancelCrashingMonitor
	a.cancelCrashingMonitor(pipelineName)

	kubeClient := a.env.GetKubeClient()
	// Delete any services associated with op.pipeline
	selector := fmt.Sprintf("%s=%s", pipelineNameLabel, pipelineName)
	opts := &metav1.DeleteOptions{
		OrphanDependents: &falseVal,
	}
	services, err := kubeClient.CoreV1().Services(a.namespace).List(metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return errors.Wrapf(err, "could not list services")
	}
	for _, service := range services.Items {
		if err := kubeClient.CoreV1().Services(a.namespace).Delete(service.Name, opts); err != nil {
			if !isNotFoundErr(err) {
				return errors.Wrapf(err, "could not delete service %q", service.Name)
			}
		}
	}
	rcs, err := kubeClient.CoreV1().ReplicationControllers(a.namespace).List(metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return errors.Wrapf(err, "could not list RCs")
	}
	for _, rc := range rcs.Items {
		if err := kubeClient.CoreV1().ReplicationControllers(a.namespace).Delete(rc.Name, opts); err != nil {
			if !isNotFoundErr(err) {
				return errors.Wrapf(err, "could not delete RC %q: %v", rc.Name)
			}
		}
	}
	return nil
}

func notifyCtx(ctx context.Context, name string) func(error, time.Duration) error {
	return func(err error, d time.Duration) error {
		select {
		case <-ctx.Done():
			return context.DeadlineExceeded
		default:
			log.Errorf("error in %s: %v: retrying in: %v", name, err, d)
		}
		return nil
	}
}

// setPipelineState is a PPS-master-specific helper that wraps
// ppsutil.SetPipelineState in a trace
func (a *apiServer) setPipelineState(ctx context.Context, pipeline string, state pps.PipelineState, reason string) (retErr error) {
	span, ctx := tracing.AddSpanToAnyExisting(ctx,
		"/pps.Master/SetPipelineState", "pipeline", pipeline, "new-state", state)
	defer func() {
		tracing.TagAnySpan(span, "err", retErr)
		tracing.FinishAnySpan(span)
	}()
	return ppsutil.SetPipelineState(ctx, a.env.GetEtcdClient(), a.pipelines,
		pipeline, nil, state, reason)
}

// transitionPipelineState is similar to setPipelineState, except that it sets
// 'from' and logs a different trace
func (a *apiServer) transitionPipelineState(ctx context.Context, pipeline string, from, to pps.PipelineState, reason string) (retErr error) {
	span, ctx := tracing.AddSpanToAnyExisting(ctx,
		"/pps.Master/TransitionPipelineState", "pipeline", pipeline,
		"from-state", from, "to-state", to)
	defer func() {
		tracing.TagAnySpan(span, "err", retErr)
		tracing.FinishAnySpan(span)
	}()
	return ppsutil.SetPipelineState(ctx, a.env.GetEtcdClient(), a.pipelines,
		pipeline, &from, to, reason)
}

func (a *apiServer) monitorPipeline(pachClient *client.APIClient, pipelineInfo *pps.PipelineInfo) {
	log.Printf("PPS master: monitoring pipeline %q", pipelineInfo.Pipeline.Name)
	// If this exits (e.g. b/c Standby is false, and pipeline has no cron inputs),
	// remove this fn's cancel() call from a.monitorCancels (if it hasn't already
	// been removed, e.g. by deletePipelineResources cancelling this call), so
	// that it can be called again
	defer a.cancelMonitor(pipelineInfo.Pipeline.Name)
	var eg errgroup.Group
	pps.VisitInput(pipelineInfo.Input, func(in *pps.Input) {
		if in.Cron != nil {
			eg.Go(func() error {
				return backoff.RetryNotify(func() error {
					return a.makeCronCommits(pachClient, in)
				}, backoff.NewInfiniteBackOff(), notifyCtx(pachClient.Ctx(), "cron for "+in.Cron.Name))
			})
		}
	})
	if pipelineInfo.Standby {
		// Capacity 1 gives us a bit of buffer so we don't needlessly go into
		// standby when SubscribeCommit takes too long to return.
		ciChan := make(chan *pfs.CommitInfo, 1)
		eg.Go(func() error {
			return backoff.RetryNotify(func() error {
				return pachClient.SubscribeCommitF(pipelineInfo.Pipeline.Name, "",
					client.NewCommitProvenance(ppsconsts.SpecRepo, pipelineInfo.Pipeline.Name, pipelineInfo.SpecCommit.ID),
					"", pfs.CommitState_READY, func(ci *pfs.CommitInfo) error {
						ciChan <- ci
						return nil
					})
			}, backoff.NewInfiniteBackOff(), notifyCtx(pachClient.Ctx(), "SubscribeCommit"))
		})
		eg.Go(func() error {
			return backoff.RetryNotify(func() error {
				span, ctx := extended.AddPipelineSpanToAnyTrace(pachClient.Ctx(),
					a.env.GetEtcdClient(), pipelineInfo.Pipeline.Name, "/pps.Master/MonitorPipeline",
					"standby", pipelineInfo.Standby)
				if span != nil {
					pachClient = pachClient.WithCtx(ctx)
				}
				defer tracing.FinishAnySpan(span)

				if err := a.transitionPipelineState(pachClient.Ctx(),
					pipelineInfo.Pipeline.Name,
					pps.PipelineState_PIPELINE_RUNNING,
					pps.PipelineState_PIPELINE_STANDBY, ""); err != nil {
					var pte ppsutil.PipelineTransitionError
					if errors.As(err, &pte) && pte.Current == pps.PipelineState_PIPELINE_PAUSED {
						// pipeline is stopped, exit monitorPipeline (which pausing the
						// pipeline should also do). monitorPipeline will be called when
						// it transitions back to running
						// TODO(msteffen): this should happen in the pipeline
						// controller
						return nil
					}
					return err
				}
				var (
					childSpan     opentracing.Span
					oldCtx        = ctx
					oldPachClient = pachClient
				)
				defer func() {
					tracing.FinishAnySpan(childSpan) // Finish any dangling children of 'span'
				}()
				for {
					// finish span from previous loops
					tracing.FinishAnySpan(childSpan)
					childSpan = nil

					var ci *pfs.CommitInfo
					select {
					case ci = <-ciChan:
						if ci.Finished != nil {
							continue
						}
						childSpan, ctx = tracing.AddSpanToAnyExisting(
							oldCtx, "/pps.Master/MonitorPipeline_SpinUp",
							"pipeline", pipelineInfo.Pipeline.Name, "commit", ci.Commit.ID)
						if childSpan != nil {
							pachClient = oldPachClient.WithCtx(ctx)
						}

						if err := a.transitionPipelineState(pachClient.Ctx(),
							pipelineInfo.Pipeline.Name,
							pps.PipelineState_PIPELINE_STANDBY,
							pps.PipelineState_PIPELINE_RUNNING, ""); err != nil {

							var pte ppsutil.PipelineTransitionError
							if errors.As(err, &pte) && pte.Current == pps.PipelineState_PIPELINE_PAUSED {
								// pipeline is stopped, exit monitorPipeline (see above)
								return nil
							}
							return err
						}

						// Stay running while commits are available
					running:
						for {
							// Wait for the commit to be finished before blocking on the
							// job because the job may not exist yet.
							if _, err := pachClient.BlockCommit(ci.Commit.Repo.Name, ci.Commit.ID); err != nil {
								return err
							}
							if _, err := pachClient.InspectJobOutputCommit(ci.Commit.Repo.Name, ci.Commit.ID, true); err != nil {
								return err
							}

							select {
							case ci = <-ciChan:
							default:
								break running
							}
						}

						if err := a.transitionPipelineState(pachClient.Ctx(),
							pipelineInfo.Pipeline.Name,
							pps.PipelineState_PIPELINE_RUNNING,
							pps.PipelineState_PIPELINE_STANDBY, ""); err != nil {

							var pte ppsutil.PipelineTransitionError
							if errors.As(err, &pte) && pte.Current == pps.PipelineState_PIPELINE_PAUSED {
								// pipeline is stopped; monitorPipeline will be called when it
								// transitions back to running
								// TODO(msteffen): this should happen in the pipeline
								// controller
								return nil
							}
							return err
						}
					case <-pachClient.Ctx().Done():
						return context.DeadlineExceeded
					}
				}
			}, backoff.NewInfiniteBackOff(), func(err error, d time.Duration) error {
				select {
				case <-pachClient.Ctx().Done():
					return context.DeadlineExceeded
				default:
					log.Printf("error in monitorPipeline: %v: retrying in: %v", err, d)
				}
				return nil
			})
		})
	}
	if err := eg.Wait(); err != nil {
		log.Printf("error in monitorPipeline: %v", err)
	}
}

func (a *apiServer) monitorCrashingPipeline(ctx context.Context, op *pipelineOp) {
	defer a.cancelMonitor(op.name)
For:
	for {
		select {
		case <-time.After(crashingBackoff):
		case <-ctx.Done():
			break For
		}
		time.Sleep(crashingBackoff)
		workersUp, err := op.allWorkersUp()
		if err != nil {
			log.Printf("error in monitorCrashingPipeline: %v", err)
			continue
		}
		if workersUp {
			if err := a.transitionPipelineState(ctx, op.name,
				pps.PipelineState_PIPELINE_CRASHING,
				pps.PipelineState_PIPELINE_RUNNING, ""); err != nil {

				var pte ppsutil.PipelineTransitionError
				if errors.As(err, &pte) && pte.Current == pps.PipelineState_PIPELINE_CRASHING {
					log.Print(err) // Pipeline has moved to STOPPED or been updated--give up
					return
				}
				log.Printf("error in monitorCrashingPipeline: %v", err)
				continue
			}
			break
		}
	}
}

func (a *apiServer) getLatestCronTime(pachClient *client.APIClient, in *pps.Input) (time.Time, error) {
	var latestTime time.Time
	files, err := pachClient.ListFile(in.Cron.Repo, "master", "")
	if err != nil && !pfsServer.IsNoHeadErr(err) {
		return latestTime, err
	} else if err != nil || len(files) == 0 {
		// File not found, this happens the first time the pipeline is run
		latestTime, err = types.TimestampFromProto(in.Cron.Start)
		if err != nil {
			return latestTime, err
		}
	} else {
		// Take the name of the most recent file as the latest timestamp
		// ListFile returns the files in lexicographical order, and the RFC3339 format goes
		// from largest unit of time to smallest, so the most recent file will be the last one
		latestTime, err = time.Parse(time.RFC3339, path.Base(files[len(files)-1].File.Path))
		if err != nil {
			return latestTime, err
		}
	}
	return latestTime, nil
}

// makeCronCommits makes commits to a single cron input's repo. It's
// a helper function called by monitorPipeline.
func (a *apiServer) makeCronCommits(pachClient *client.APIClient, in *pps.Input) error {
	schedule, err := cron.ParseStandard(in.Cron.Spec)
	if err != nil {
		return err // Shouldn't happen, as the input is validated in CreatePipeline
	}
	// make sure there isn't an unfinished commit on the branch
	commitInfo, err := pachClient.InspectCommit(in.Cron.Repo, "master")
	if err != nil && !pfsServer.IsNoHeadErr(err) {
		return err
	} else if commitInfo != nil && commitInfo.Finished == nil {
		// and if there is, delete it
		if err = pachClient.DeleteCommit(in.Cron.Repo, commitInfo.Commit.ID); err != nil {
			return err
		}
	}

	latestTime, err := a.getLatestCronTime(pachClient, in)
	if err != nil {
		return err
	}

	for {
		// get the time of the next time from the latest time using the cron schedule
		next := schedule.Next(latestTime)
		// and wait until then to make the next commit
		select {
		case <-time.After(time.Until(next)):
		case <-pachClient.Ctx().Done():
			return pachClient.Ctx().Err()
		}
		if err != nil {
			return err
		}

		// We need the DeleteFile and the PutFile to happen in the same commit
		_, err = pachClient.StartCommit(in.Cron.Repo, "master")
		if err != nil {
			return err
		}
		if in.Cron.Overwrite {
			// get rid of any files, so the new file "overwrites" previous runs
			err = pachClient.DeleteFile(in.Cron.Repo, "master", "")
			if err != nil && !isNotFoundErr(err) && !pfsServer.IsNoHeadErr(err) {
				return errors.Wrapf(err, "delete error")
			}
		}

		// Put in an empty file named by the timestamp
		_, err = pachClient.PutFile(in.Cron.Repo, "master", next.Format(time.RFC3339), strings.NewReader(""))
		if err != nil {
			return errors.Wrapf(err, "put error")
		}

		err = pachClient.FinishCommit(in.Cron.Repo, "master")
		if err != nil {
			return err
		}

		// set latestTime to the next time
		latestTime = next
	}
}
