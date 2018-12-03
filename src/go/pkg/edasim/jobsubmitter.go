package edasim

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/azure/avere/src/go/pkg/azure"
	"github.com/azure/avere/src/go/pkg/file"
)

// JobSubmitter defines the structure used for the job submitter process
type JobSubmitter struct {
	Context       context.Context
	BatchName     string
	ID            int
	ReadyQueue    *azure.Queue
	JobCount      int
	JobPath       string
	JobFileSizeKB int
}

// InitializeJobSubmitter initializes the job submitter structure
func InitializeJobSubmitter(ctx context.Context, batchName string, id int, readyQueue *azure.Queue, jobCount int, jobPath string, jobFileSizeKB int) *JobSubmitter {
	return &JobSubmitter{
		Context:       ctx,
		BatchName:     batchName,
		ID:            id,
		ReadyQueue:    readyQueue,
		JobCount:      jobCount,
		JobPath:       jobPath,
		JobFileSizeKB: jobFileSizeKB,
	}
}

// Run is the entry point for the JobSubmitter go routine
func (j *JobSubmitter) Run(syncWaitGroup *sync.WaitGroup) {
	defer syncWaitGroup.Done()
	log.Printf("JobSubmitter %d: starting to submit %d jobs\n", j.ID, j.JobCount)

	writer := j.Context.Value(ReaderWriterContextKey).(*file.ReaderWriter)

	for i := 0; i < j.JobCount; i++ {
		jobConfigFile := InitializeJobConfigFile(j.getJobName(i))
		jobFilePath, err := jobConfigFile.WriteJobConfigFile(writer, j.JobPath, j.JobFileSizeKB)
		if err != nil {
			log.Printf("ERROR writing job file: %v", err)
			continue
		}

		// queue completion
		if err := j.ReadyQueue.Enqueue(jobFilePath); err != nil {
			log.Printf("ERROR enqueuing message '%s': %v", jobFilePath, err)
			continue
		}
	}

	log.Printf("user %d: completed submitting %d jobs\n", j.ID, j.JobCount)
}

func (j *JobSubmitter) getJobName(index int) string {
	return fmt.Sprintf("%d_%d", j.ID, index)
}
