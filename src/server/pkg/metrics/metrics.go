package metrics

import (
	"fmt"
	"strings"
	"time"

	"github.com/pachyderm/pachyderm/src/client/pkg/config"
	"github.com/pachyderm/pachyderm/src/client/pkg/uuid"
	"github.com/pachyderm/pachyderm/src/client/version"

	log "github.com/Sirupsen/logrus"
	"github.com/segmentio/analytics-go"
	"golang.org/x/net/context"
	"google.golang.org/grpc/metadata"
	kube "k8s.io/kubernetes/pkg/client/unversioned"
)

//Reporter is used to submit user & cluster metrics to segment
type Reporter struct {
	segmentClient *analytics.Client
	clusterID     string
	kubeClient    *kube.Client
}

// NewReporter creates a new reporter and kicks off the loop to report cluster
// metrics
func NewReporter(clusterID string, kubeClient *kube.Client) *Reporter {
	reporter := &Reporter{
		segmentClient: newPersistentClient(),
		clusterID:     clusterID,
		kubeClient:    kubeClient,
	}
	go reporter.reportClusterMetrics()
	return reporter
}

//ReportUserAction pushes the action into a queue for reporting,
// and reports the start, finish, and error conditions
func ReportUserAction(ctx context.Context, r *Reporter, action string) func(time.Time, error) {
	if r == nil {
		// This happens when stubbing out metrics for testing, e.g. src/server/pfs/server/server_test.go
		return func(time.Time, error) {}
	}
	// If we report nil, segment sees it, but mixpanel omits the field
	r.reportUserAction(ctx, fmt.Sprintf("%vStarted", action), 1)
	return func(start time.Time, err error) {
		if err == nil {
			r.reportUserAction(ctx, fmt.Sprintf("%vFinished", action), time.Since(start).Seconds())
		} else {
			r.reportUserAction(ctx, fmt.Sprintf("%vErrored", action), err.Error())
		}
	}
}

func getKeyFromMD(md metadata.MD, key string) (string, error) {
	if md[key] != nil && len(md[key]) > 0 {
		return md[key][0], nil
	}
	return "", fmt.Errorf("error extracting userid from metadata. userid is empty")
}

func (r *Reporter) reportUserAction(ctx context.Context, action string, value interface{}) {
	md, ok := metadata.FromContext(ctx)
	if ok {
		// metadata API downcases all the key names
		userID, err := getKeyFromMD(md, "userid")
		if err != nil {
			// The FUSE client will never have a userID, so normal usage will produce a lot of these errors
			return
		}
		prefix, err := getKeyFromMD(md, "prefix")
		if err != nil {
			log.Errorln(err)
			return
		}
		reportUserMetricsToSegment(
			r.segmentClient,
			userID,
			prefix,
			action,
			value,
			r.clusterID,
		)
	}
}

// ReportAndFlushUserAction immediately reports the metric
// It is used in the few places we need to report metrics from the client.
func ReportAndFlushUserAction(action string, value interface{}) func() {
	metricsDone := make(chan struct{})
	go func() {
		fmt.Printf("gonna get report metrics for %v\n", action)
		if strings.Contains(action, "Finished") {
			fmt.Printf("gonna waiy for %v\n", action)
			fmt.Printf("tick: %v\n", time.Now())
			time.Sleep(3 * time.Second)
			fmt.Printf("tock: %v\n", time.Now())
		}
		client := newSegmentClient()
		defer client.Close()
		cfg, err := config.Read()
		if err != nil {
			log.Errorf("Error reading userid from ~/.pachyderm/config: %v", err)
			// metrics errors are non fatal
			return
		}
		reportUserMetricsToSegment(client, cfg.UserID, "user", action, value, "")
		close(metricsDone)
	}()
	return func() {
		fmt.Printf("executing wait for action: %v, value: %v\n", action, value)
		select {
		case <-metricsDone:
			fmt.Printf("metrics call completed for %v\n", action)
			return
		case <-time.After(time.Second * 5):
			fmt.Printf("metrics call timed out for %v\n", action)
			return
		}
	}
}

func StartReportAndFlushUserAction(action string, value interface{}) func() {
	return ReportAndFlushUserAction(fmt.Sprintf("%vStarted", action), value)
}

func FinishReportAndFlushUserAction(action string, err error, start time.Time) func() {
	var wait func()
	if err != nil {
		wait = ReportAndFlushUserAction(fmt.Sprintf("%vErrored", action), err)
	} else {
		wait = ReportAndFlushUserAction(fmt.Sprintf("%vFinished", action), time.Since(start).Seconds())
	}
	return wait
}

func (r *Reporter) reportClusterMetrics() {
	for {
		time.Sleep(reportingInterval)
		metrics := &Metrics{}
		externalMetrics(r.kubeClient, metrics)
		metrics.ClusterID = r.clusterID
		metrics.PodID = uuid.NewWithoutDashes()
		metrics.Version = version.PrettyPrintVersion(version.Version)
		reportClusterMetricsToSegment(r.segmentClient, metrics)
	}
}
