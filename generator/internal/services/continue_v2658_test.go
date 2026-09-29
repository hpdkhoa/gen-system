// D089: RejectContinue marked a running job failed even when no review was
// pending, so a second run could start while the first still wrote files.
package services

import "testing"

func TestRejectContinue_NoPendingKeepsRunningJob(t *testing.T) {
	s := &BuildService{job: &BuildJob{Status: JobRunning}}
	if err := s.RejectContinue(); err == nil {
		t.Fatal("want an error when no continuation is pending")
	}
	if s.job.Status != JobRunning || s.job.Error != "" {
		t.Fatalf("running job changed: status %q error %q", s.job.Status, s.job.Error)
	}
}
