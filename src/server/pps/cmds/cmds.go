package cmds

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/Jeffail/gabs"
	"github.com/golang/protobuf/jsonpb"
	"github.com/pachyderm/pachyderm"
	pach "github.com/pachyderm/pachyderm/src/client"
	ppsclient "github.com/pachyderm/pachyderm/src/client/pps"
	pkgcmd "github.com/pachyderm/pachyderm/src/server/pkg/cmd"
	"github.com/pachyderm/pachyderm/src/server/pps/example"
	"github.com/pachyderm/pachyderm/src/server/pps/pretty"
	"github.com/spf13/cobra"
	"golang.org/x/net/context"
)

type ByCreationTime []*ppsclient.JobInfo

func (arr ByCreationTime) Len() int { return len(arr) }

func (arr ByCreationTime) Swap(i, j int) { arr[i], arr[j] = arr[j], arr[i] }

func (arr ByCreationTime) Less(i, j int) bool {
	if arr[i].CreatedAt == nil || arr[j].CreatedAt == nil {
		return false
	}

	if arr[i].CreatedAt.Seconds < arr[j].CreatedAt.Seconds {
		return true
	} else if arr[i].CreatedAt.Seconds == arr[j].CreatedAt.Seconds {
		return arr[i].CreatedAt.Nanos < arr[j].CreatedAt.Nanos
	}

	return false
}

func Cmds(address string) ([]*cobra.Command, error) {
	marshaller := &jsonpb.Marshaler{Indent: "  "}

	job := &cobra.Command{
		Use:   "job",
		Short: "Docs for jobs.",
		Long: `Jobs are the basic unit of computation in Pachyderm.

Jobs run a containerized workload over a set of finished input commits.
Creating a job will also create a new repo and a commit in that repo which
contains the output of the job. Unless the job is created with another job as a
parent. If the job is created with a parent it will use the same repo as its
parent job and the commit it creates will use the parent job's commit as a
parent.
If the job fails the commit it creates will not be finished.
The increase the throughput of a job increase the Shard paremeter.
`,
		Run: pkgcmd.RunFixedArgs(0, func(args []string) error {
			return nil
		}),
	}

	exampleCreateJobRequest, err := marshaller.MarshalToString(example.CreateJobRequest())
	if err != nil {
		return nil, err
	}

	exampleRunPipelineSpec, err := marshaller.MarshalToString(example.RunPipelineSpec())
	if err != nil {
		return nil, err
	}

	pipelineSpec := string(pachyderm.MustAsset("doc/pipeline_spec.md"))

	var jobPath string
	createJob := &cobra.Command{
		Use:   "create-job -f job.json",
		Short: "Create a new job. Returns the id of the created job.",
		Long:  fmt.Sprintf("Create a new job from a spec, the spec looks like this\n%s", exampleCreateJobRequest),
		Run: func(cmd *cobra.Command, args []string) {
			client, err := pach.NewFromAddress(address)
			if err != nil {
				pkgcmd.ErrorAndExit("Error connecting to pps: %v", err)
			}
			var buf bytes.Buffer
			var jobReader io.Reader
			if jobPath == "-" {
				jobReader = io.TeeReader(os.Stdin, &buf)
				fmt.Print("Reading from stdin.\n")
			} else {
				jobFile, err := os.Open(jobPath)
				if err != nil {
					pkgcmd.ErrorAndExit("Error opening %s: %v", jobPath, err)
				}
				defer func() {
					if err := jobFile.Close(); err != nil {
						pkgcmd.ErrorAndExit("Error closing%s: %v", jobPath, err)
					}
				}()
				jobReader = io.TeeReader(jobFile, &buf)
			}
			var request ppsclient.CreateJobRequest
			decoder := json.NewDecoder(jobReader)
			s, err := replaceMethodAliases(decoder)
			if err != nil {
				err = describeSyntaxError(err, buf)
				pkgcmd.ErrorAndExit("Error parsing job spec: %v", err)
			}
			if err := jsonpb.UnmarshalString(s, &request); err != nil {
				pkgcmd.ErrorAndExit("Error reading from stdin: %v", err)
			}
			job, err := client.PpsAPIClient.CreateJob(
				context.Background(),
				&request,
			)
			if err != nil {
				pkgcmd.ErrorAndExit("Error from CreateJob: %v", err)
			}
			fmt.Println(job.ID)
		},
	}
	createJob.Flags().StringVarP(&jobPath, "file", "f", "-", "The file containing the job, - reads from stdin.")

	var block bool
	inspectJob := &cobra.Command{
		Use:   "inspect-job job-id",
		Short: "Return info about a job.",
		Long:  "Return info about a job.",
		Run: pkgcmd.RunFixedArgs(1, func(args []string) error {
			client, err := pach.NewFromAddress(address)
			if err != nil {
				return err
			}
			jobInfo, err := client.InspectJob(args[0], block)
			if err != nil {
				pkgcmd.ErrorAndExit("Error from InspectJob: %s", err.Error())
			}
			if jobInfo == nil {
				pkgcmd.ErrorAndExit("Job %s not found.", args[0])
			}
			return pretty.PrintDetailedJobInfo(jobInfo)
		}),
	}
	inspectJob.Flags().BoolVarP(&block, "block", "b", false, "block until the job has either succeeded or failed")

	var pipelineName string
	listJob := &cobra.Command{
		Use:   "list-job [-p pipeline-name] [commits]",
		Short: "Return info about jobs.",
		Long: `Return info about jobs.

Examples:

	# return all jobs
	$ pachctl list-job

	# return all jobs in pipeline foo
	$ pachctl list-job -p foo

	# return all jobs whose input commits include foo/abc123 and bar/def456
	$ pachctl list-job foo/abc123 bar/def456

	# return all jobs in pipeline foo and whose input commits include bar/def456
	$ pachctl list-job -p foo bar/def456

`,
		Run: func(cmd *cobra.Command, args []string) {
			client, err := pach.NewFromAddress(address)
			if err != nil {
				pkgcmd.ErrorAndExit("Error from InspectJob: %v", err)
			}

			commits, err := pkgcmd.ParseCommits(args)
			if err != nil {
				cmd.Usage()
				pkgcmd.ErrorAndExit("Error from InspectJob: %v", err)
			}

			jobInfos, err := client.ListJob(pipelineName, commits)
			if err != nil {
				pkgcmd.ErrorAndExit("Error from InspectJob: %v", err)
			}

			// Display newest jobs first
			sort.Sort(sort.Reverse(ByCreationTime(jobInfos)))

			writer := tabwriter.NewWriter(os.Stdout, 20, 1, 3, ' ', 0)
			pretty.PrintJobHeader(writer)
			for _, jobInfo := range jobInfos {
				pretty.PrintJobInfo(writer, jobInfo)
			}

			if err := writer.Flush(); err != nil {
				pkgcmd.ErrorAndExit("Error from InspectJob: %v", err)
			}
		},
	}
	listJob.Flags().StringVarP(&pipelineName, "pipeline", "p", "", "Limit to jobs made by pipeline.")

	getLogs := &cobra.Command{
		Use:   "get-logs job-id",
		Short: "Return logs from a job.",
		Long:  "Return logs from a job.",
		Run: pkgcmd.RunFixedArgs(1, func(args []string) error {
			client, err := pach.NewFromAddress(address)
			if err != nil {
				return err
			}
			return client.GetLogs(args[0], os.Stdout)
		}),
	}

	pipeline := &cobra.Command{
		Use:   "pipeline",
		Short: "Docs for pipelines.",
		Long: `Pipelines are a powerful abstraction for automating jobs.

Pipelines take a set of repos as inputs, rather than the set of commits that
jobs take. Pipelines then subscribe to commits on those repos and launches a job
to process each incoming commit.
Creating a pipeline will also create a repo of the same name.
All jobs created by a pipeline will create commits in the pipeline's repo.
`,
		Run: pkgcmd.RunFixedArgs(0, func(args []string) error {
			return nil
		}),
	}

	var pipelinePath string
	if err != nil {
		return nil, err
	}
	createPipeline := &cobra.Command{
		Use:   "create-pipeline -f pipeline.json",
		Short: "Create a new pipeline.",
		Long:  fmt.Sprintf("Create a new pipeline from a spec\n\n%s", pipelineSpec),
		Run: func(cmd *cobra.Command, args []string) {
			client, err := pach.NewFromAddress(address)
			if err != nil {
				pkgcmd.ErrorAndExit("Error connecting to pps: %s", err.Error())
			}
			var buf bytes.Buffer
			var pipelineReader io.Reader
			if pipelinePath == "-" {
				pipelineReader = io.TeeReader(os.Stdin, &buf)
				fmt.Print("Reading from stdin.\n")
			} else {
				rawBytes, err := ioutil.ReadFile(pipelinePath)
				if err != nil {
					pkgcmd.ErrorAndExit("Error reading file %s", pipelinePath)
				}

				pipelineReader = io.TeeReader(strings.NewReader(string(rawBytes)), &buf)
			}
			var request ppsclient.CreatePipelineRequest
			decoder := json.NewDecoder(pipelineReader)
			for {
				s, err := replaceMethodAliases(decoder)
				if err != nil {
					if err == io.EOF {
						break
					}
					err = describeSyntaxError(err, buf)
					pkgcmd.ErrorAndExit("Error parsing pipeline spec: %v", err)
				}
				if err := jsonpb.UnmarshalString(s, &request); err != nil {
					pkgcmd.ErrorAndExit("Error marshalling JSON into protobuf: %v", err)
				}
				if _, err := client.PpsAPIClient.CreatePipeline(
					context.Background(),
					&request,
				); err != nil {
					pkgcmd.ErrorAndExit("Error from CreatePipeline: %v", err)
				}
			}
		},
	}
	createPipeline.Flags().StringVarP(&pipelinePath, "file", "f", "-", "The file containing the pipeline, - reads from stdin.")

	inspectPipeline := &cobra.Command{
		Use:   "inspect-pipeline pipeline-name",
		Short: "Return info about a pipeline.",
		Long:  "Return info about a pipeline.",
		Run: pkgcmd.RunFixedArgs(1, func(args []string) error {
			client, err := pach.NewFromAddress(address)
			if err != nil {
				return err
			}
			pipelineInfo, err := client.InspectPipeline(args[0])
			if err != nil {
				pkgcmd.ErrorAndExit("Error from InspectPipeline: %s", err.Error())
			}
			if pipelineInfo == nil {
				pkgcmd.ErrorAndExit("Pipeline %s not found.", args[0])
			}
			return pretty.PrintDetailedPipelineInfo(pipelineInfo)
		}),
	}

	listPipeline := &cobra.Command{
		Use:   "list-pipeline",
		Short: "Return info about all pipelines.",
		Long:  "Return info about all pipelines.",
		Run: pkgcmd.RunFixedArgs(0, func(args []string) error {
			client, err := pach.NewFromAddress(address)
			if err != nil {
				return err
			}
			pipelineInfos, err := client.ListPipeline()
			if err != nil {
				pkgcmd.ErrorAndExit("Error from ListPipeline: %s", err.Error())
			}
			writer := tabwriter.NewWriter(os.Stdout, 20, 1, 3, ' ', 0)
			pretty.PrintPipelineHeader(writer)
			for _, pipelineInfo := range pipelineInfos {
				pretty.PrintPipelineInfo(writer, pipelineInfo)
			}
			return writer.Flush()
		}),
	}

	deletePipeline := &cobra.Command{
		Use:   "delete-pipeline pipeline-name",
		Short: "Delete a pipeline.",
		Long:  "Delete a pipeline.",
		Run: pkgcmd.RunFixedArgs(1, func(args []string) error {
			client, err := pach.NewFromAddress(address)
			if err != nil {
				return err
			}
			if err := client.DeletePipeline(args[0]); err != nil {
				pkgcmd.ErrorAndExit("Error from DeletePipeline: %s", err.Error())
			}
			return nil
		}),
	}

	var specPath string
	runPipeline := &cobra.Command{
		Use:   "run-pipeline pipeline-name [-f job.json]",
		Short: "Run a pipeline once.",
		Long:  fmt.Sprintf("Run a pipeline once, optionally overriding some pipeline options by providing a spec.  The spec looks like this:\n%s", exampleRunPipelineSpec),
		Run: pkgcmd.RunFixedArgs(1, func(args []string) error {
			client, err := pach.NewFromAddress(address)
			if err != nil {
				return err
			}

			request := &ppsclient.CreateJobRequest{
				Pipeline: &ppsclient.Pipeline{
					Name: args[0],
				},
				Force: true,
			}

			var buf bytes.Buffer
			var specReader io.Reader
			if specPath == "-" {
				specReader = io.TeeReader(os.Stdin, &buf)
				fmt.Print("Reading from stdin.\n")
			} else if specPath != "" {
				specFile, err := os.Open(specPath)
				if err != nil {
					pkgcmd.ErrorAndExit("Error opening %s: %v", specPath, err)
				}

				defer func() {
					if err := specFile.Close(); err != nil {
						pkgcmd.ErrorAndExit("Error closing%s: %v", specPath, err)
					}
				}()

				specReader = io.TeeReader(specFile, &buf)
				decoder := json.NewDecoder(specReader)
				s, err := replaceMethodAliases(decoder)
				if err != nil {
					err = describeSyntaxError(err, buf)
					pkgcmd.ErrorAndExit("Error parsing pipeline spec: %v", err)
				}

				if err := jsonpb.UnmarshalString(s, request); err != nil {
					pkgcmd.ErrorAndExit("Error reading from stdin: %v", err)
				}
			}

			job, err := client.PpsAPIClient.CreateJob(
				context.Background(),
				request,
			)
			if err != nil {
				pkgcmd.ErrorAndExit("Error from RunPipeline: %v", err)
			}
			fmt.Println(job.ID)
			return nil
		}),
	}
	runPipeline.Flags().StringVarP(&specPath, "file", "f", "", "The file containing the run-pipeline spec, - reads from stdin.")

	var result []*cobra.Command
	result = append(result, job)
	result = append(result, createJob)
	result = append(result, inspectJob)
	result = append(result, getLogs)
	result = append(result, listJob)
	result = append(result, pipeline)
	result = append(result, createPipeline)
	result = append(result, inspectPipeline)
	result = append(result, listPipeline)
	result = append(result, deletePipeline)
	result = append(result, runPipeline)
	return result, nil
}

func describeSyntaxError(originalErr error, parsedBuffer bytes.Buffer) error {

	sErr, ok := originalErr.(*json.SyntaxError)
	if !ok {
		return originalErr
	}

	buffer := make([]byte, sErr.Offset)
	parsedBuffer.Read(buffer)

	lineOffset := strings.LastIndex(string(buffer[:len(buffer)-1]), "\n")
	if lineOffset == -1 {
		lineOffset = 0
	}

	lines := strings.Split(string(buffer[:len(buffer)-1]), "\n")
	lineNumber := len(lines)

	descriptiveErrorString := fmt.Sprintf("Syntax Error on line %v:\n%v\n%v^\n%v\n",
		lineNumber,
		string(buffer[lineOffset:]),
		strings.Repeat(" ", int(sErr.Offset)-2-lineOffset),
		originalErr,
	)

	return errors.New(descriptiveErrorString)
}

func replaceMethodAliases(decoder *json.Decoder) (string, error) {
	// We want to allow for a syntactic suger where the user
	// can specify a method with a string such as "map" or "reduce".
	// To that end, we check for the "method" field and replace
	// the string with an actual method object before we unmarshal
	// the json spec into a protobuf message
	pipeline, err := gabs.ParseJSONDecoder(decoder)
	if err != nil {
		return "", err
	}

	// No need to do anything if the pipeline does not specify inputs
	if !pipeline.ExistsP("inputs") {
		return pipeline.String(), nil
	}

	inputs := pipeline.S("inputs")
	children, err := inputs.Children()
	if err != nil {
		return "", err
	}
	for _, input := range children {
		if !input.ExistsP("method") {
			continue
		}
		methodAlias, ok := input.S("method").Data().(string)
		if ok {
			strat, ok := pach.MethodAliasMap[methodAlias]
			if ok {
				input.Set(strat, "method")
			} else {
				return "", fmt.Errorf("unrecognized input alias: %s", methodAlias)
			}
		} else {
			if !(input.ExistsP("method.partition") && input.ExistsP("method.incremental")) {
				return "", fmt.Errorf("an input method needs to be either a string alias or a json object; please read the pipeline specification for details")
			}
			partition, ok := input.S("method", "partition").Data().(string)
			if ok {
				switch partition {
				case "block":
					input.SetP(ppsclient.Partition_BLOCK, "method.partition")
				case "file":
					input.SetP(ppsclient.Partition_FILE, "method.partition")
				case "repo":
					input.SetP(ppsclient.Partition_REPO, "method.partition")
				default:
					return "", fmt.Errorf("partition needs to be 'block', 'file', or 'repo'; got %s instead", partition)
				}
			} else {
				return "", fmt.Errorf("partition needs to be a string")
			}
		}
	}

	return pipeline.String(), nil
}
