package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/azure/avere/src/go/pkg/azure"
	"github.com/azure/avere/src/go/pkg/cli"
	"github.com/azure/avere/src/go/pkg/edasim"
	"github.com/azure/avere/src/go/pkg/log"
)

func usage(errs ...error) {
	for _, err := range errs {
		fmt.Fprintf(os.Stderr, "error: %s\n\n", err.Error())
	}
	fmt.Fprintf(os.Stderr, "usage: %s [OPTIONS]\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "       write the job config file and posts to the queue\n")
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "required env vars:\n")
	fmt.Fprintf(os.Stderr, "\t%s - azure storage account\n", azure.AZURE_STORAGE_ACCOUNT)
	fmt.Fprintf(os.Stderr, "\t%s - azure storage account key\n", azure.AZURE_STORAGE_ACCOUNT_KEY)
	fmt.Fprintf(os.Stderr, "\t%s - azure event hub sender name\n", azure.AZURE_EVENTHUB_SENDERKEYNAME)
	fmt.Fprintf(os.Stderr, "\t%s - azure event hub sender key\n", azure.AZURE_EVENTHUB_SENDERKEY)
	fmt.Fprintf(os.Stderr, "\t%s - azure event hub namespace name\n", azure.AZURE_EVENTHUB_NAMESPACENAME)
	fmt.Fprintf(os.Stderr, "\t%s - azure event hub hub name\n", azure.AZURE_EVENTHUB_HUBNAME)
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "options:\n")
	flag.PrintDefaults()
}

func verifyEnvVars() bool {
	available := true
	available = available && cli.VerifyEnvVar(azure.AZURE_STORAGE_ACCOUNT)
	available = available && cli.VerifyEnvVar(azure.AZURE_STORAGE_ACCOUNT_KEY)
	available = available && cli.VerifyEnvVar(azure.AZURE_EVENTHUB_SENDERKEYNAME)
	available = available && cli.VerifyEnvVar(azure.AZURE_EVENTHUB_SENDERKEY)
	available = available && cli.VerifyEnvVar(azure.AZURE_EVENTHUB_NAMESPACENAME)
	available = available && cli.VerifyEnvVar(azure.AZURE_EVENTHUB_HUBNAME)
	return available
}

func initializeApplicationVariables(ctx context.Context) (*azure.EventHubSender, *edasim.Orchestrator) {
	var enableDebugging = flag.Bool("enableDebugging", false, "enable debug logging")
	var jobReadyQueueName = flag.String("jobReadyQueueName", edasim.QueueJobReady, "the job read queue name")
	var jobProcessQueueName = flag.String("jobProcessQueueName", edasim.QueueJobProcess, "the job process queue name")
	var jobCompleteQueueName = flag.String("jobCompleteQueueName", edasim.QueueJobComplete, "the job completion queue name")
	var uploaderQueueName = flag.String("uploaderQueueName", edasim.QueueUploader, "the uploader job queue name")

	var jobStartFileConfigSizeKB = flag.Int("jobStartFileConfigSizeKB", edasim.DefaultFileSizeKB, "the job start file size in KB to write at start of job")
	var jobStartFileCount = flag.Int("jobStartFileCount", edasim.DefaultJobStartFiles, "the count of start job files")
	var jobStartFileBasePathCSV = flag.String("jobStartFileBasePathCSV", "", "one or more job file paths separated by commas, this is where the work files are written")
	var jobCompleteFileSizeKB = flag.Int("jobCompleteFileSizeKB", 384, "the job complete file size in KB to write after job completed")
	var jobCompleteFailedFileSizeKB = flag.Int("jobCompleteFailedFileSizeKB", 1024, "the job start file size in KB to write at start of job")
	var jobFailedProbability = flag.Float64("jobFailedProbability", 0.01, "the probability of a job failure")
	var jobCompleteFileCount = flag.Int("jobCompleteFileCount", 12, "the count of completed job files")

	var orchestratorThreads = flag.Int("orchestratorThreads", edasim.DefaultOrchestratorThreads, "the number of concurrent orechestratorthreads")

	flag.Parse()

	if *enableDebugging {
		log.EnableDebugging()
	}

	if envVarsAvailable := verifyEnvVars(); !envVarsAvailable {
		usage()
		os.Exit(1)
	}

	storageAccount := cli.GetEnv(azure.AZURE_STORAGE_ACCOUNT)
	storageKey := cli.GetEnv(azure.AZURE_STORAGE_ACCOUNT_KEY)
	eventHubSenderName := cli.GetEnv(azure.AZURE_EVENTHUB_SENDERKEYNAME)
	eventHubSenderKey := cli.GetEnv(azure.AZURE_EVENTHUB_SENDERKEY)
	eventHubNamespaceName := cli.GetEnv(azure.AZURE_EVENTHUB_NAMESPACENAME)
	eventHubHubName := cli.GetEnv(azure.AZURE_EVENTHUB_HUBNAME)

	if len(*jobStartFileBasePathCSV) == 0 {
		fmt.Fprintf(os.Stderr, "ERROR: jobStartFileBasePathCSV is not specified\n")
		usage()
		os.Exit(1)
	}

	jobStartFilePaths := strings.Split(*jobStartFileBasePathCSV, ",")
	for _, path := range jobStartFilePaths {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "ERROR: jobStartFilePath '%s' does not exist\n", path)
			usage()
			os.Exit(1)
		}
	}

	azure.FatalValidateQueueName(*jobReadyQueueName)
	azure.FatalValidateQueueName(*jobProcessQueueName)
	azure.FatalValidateQueueName(*jobCompleteQueueName)
	azure.FatalValidateQueueName(*uploaderQueueName)

	eventHub := edasim.InitializeReaderWriters(
		ctx,
		eventHubSenderName,
		eventHubSenderKey,
		eventHubNamespaceName,
		eventHubHubName)

	return eventHub, edasim.InitializeOrchestrator(
		ctx,
		storageAccount,
		storageKey,
		*jobReadyQueueName,
		*jobProcessQueueName,
		*jobCompleteQueueName,
		*uploaderQueueName,
		*jobStartFileConfigSizeKB,
		*jobStartFileCount,
		jobStartFilePaths,
		*jobCompleteFileSizeKB,
		*jobCompleteFailedFileSizeKB,
		*jobFailedProbability,
		*jobCompleteFileCount,
		*orchestratorThreads)
}

func main() {
	// setup the shared context
	ctx, cancel := context.WithCancel(context.Background())
	syncWaitGroup := sync.WaitGroup{}

	// initialize and start the orchestrator
	eventHub, orchestrator := initializeApplicationVariables(ctx)
	syncWaitGroup.Add(1)
	go orchestrator.Run(&syncWaitGroup)

	// wait on ctrl-c
	sigchan := make(chan os.Signal, 10)
	// catch all signals since this is to run as daemon
	signal.Notify(sigchan)
	//signal.Notify(sigchan, os.Interrupt)
	<-sigchan
	log.Info.Printf("Received ctrl-c, stopping services...")
	cancel()

	log.Info.Printf("Waiting for all processes to finish")
	syncWaitGroup.Wait()

	log.Info.Printf(" wait for the event hub sender to complete")

	for {
		if eventHub.IsSenderComplete() {
			break
		} else {
			time.Sleep(time.Duration(10) * time.Millisecond)
		}
	}

	log.Info.Printf("finished")
}
