package mailer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// createTestMail builds a valid MailContent for tests, panicking on failure
// since a failure here means the test setup itself is broken.
func createTestMail(subject string) MailContent {
	content, err := NewMailContentBuilder().
		WithFromName("Test Sender").
		WithFromAddress("sender@example.com").
		WithToName("Test Recipient").
		WithToAddress("recipient@example.com").
		WithMimeType(MimeTypeTextPlain).
		WithSubject(subject).
		WithBody("Test body content that is long enough.").
		Build()
	if err != nil {
		panic(fmt.Sprintf("createTestMail failed for subject %q: %v", subject, err))
	}
	return content
}

// MockMailerService is a configurable MailerService test double.
type MockMailerService struct {
	mu       sync.Mutex
	sent     []MailContent
	SendFunc func(ctx context.Context, content MailContent) error
	SendErr  error
}

func (m *MockMailerService) Send(ctx context.Context, content MailContent) error {
	if m.SendFunc != nil {
		return m.SendFunc(ctx, content)
	}
	m.mu.Lock()
	m.sent = append(m.sent, content)
	m.mu.Unlock()
	return m.SendErr
}

func (m *MockMailerService) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sent)
}

func TestMailQueueError_Error(t *testing.T) {
	err := &MailQueueError{Message: "bad config"}
	if err.Error() != "bad config" {
		t.Errorf("Error() = %q, want %q", err.Error(), "bad config")
	}
}

func TestNewMailService(t *testing.T) {
	mockMailer := &MockMailerService{}

	tests := []struct {
		name        string
		config      *MailServiceConfig
		expectError bool
		checkFunc   func(*testing.T, *MailService)
	}{
		{
			name: "Valid config",
			config: &MailServiceConfig{
				Ctx:         context.Background(),
				WorkerCount: 5,
				Timeout:     10 * time.Second,
				Mailer:      mockMailer,
			},
			checkFunc: func(t *testing.T, s *MailService) {
				if s.workerCount != 5 {
					t.Errorf("workerCount = %d, want 5", s.workerCount)
				}
				if cap(s.content) != 5 {
					t.Errorf("queue capacity = %d, want 5 (defaults to WorkerCount)", cap(s.content))
				}
			},
		},
		{
			name: "Custom queue size",
			config: &MailServiceConfig{
				WorkerCount: 2,
				QueueSize:   64,
				Mailer:      mockMailer,
			},
			checkFunc: func(t *testing.T, s *MailService) {
				if cap(s.content) != 64 {
					t.Errorf("queue capacity = %d, want 64", cap(s.content))
				}
			},
		},
		{
			name:        "Nil config",
			config:      nil,
			expectError: true,
		},
		{
			name:        "Nil mailer",
			config:      &MailServiceConfig{WorkerCount: 1},
			expectError: true,
		},
		{
			name:        "Worker count too low",
			config:      &MailServiceConfig{WorkerCount: 0, Mailer: mockMailer},
			expectError: true,
		},
		{
			name:        "Worker count too high",
			config:      &MailServiceConfig{WorkerCount: ValidMaxWorkerCount + 1, Mailer: mockMailer},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, err := NewMailService(tt.config)
			if tt.expectError {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				var qErr *MailQueueError
				if !errors.As(err, &qErr) {
					t.Errorf("expected *MailQueueError, got %T", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.checkFunc != nil {
				tt.checkFunc(t, service)
			}
		})
	}
}

func TestMailService_StartStopEnqueue(t *testing.T) {
	mockMailer := &MockMailerService{}
	service, err := NewMailService(&MailServiceConfig{WorkerCount: 3, Mailer: mockMailer})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	service.Start()

	const numEmails = 10
	for i := range numEmails {
		if err := service.Enqueue(createTestMail(fmt.Sprintf("Test %d", i))); err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}
	}

	// Stop drains the queue and waits for workers before returning.
	service.Stop()

	if mockMailer.Count() != numEmails {
		t.Errorf("sent %d emails, want %d", mockMailer.Count(), numEmails)
	}
}

func TestMailService_StartIsIdempotent(t *testing.T) {
	mockMailer := &MockMailerService{}
	service, err := NewMailService(&MailServiceConfig{WorkerCount: 2, Mailer: mockMailer})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	service.Start()
	service.Start() // second call must be a no-op

	if err := service.Enqueue(createTestMail("idem")); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}
	service.Stop()

	if mockMailer.Count() != 1 {
		t.Errorf("sent %d emails, want 1", mockMailer.Count())
	}
}

func TestMailService_StopIsIdempotent(t *testing.T) {
	service, err := NewMailService(&MailServiceConfig{WorkerCount: 1, Mailer: &MockMailerService{}})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	service.Start()
	service.Stop()
	service.Stop() // must not panic on a double close
}

func TestMailService_StopRejectsEnqueue(t *testing.T) {
	service, err := NewMailService(&MailServiceConfig{WorkerCount: 1, Mailer: &MockMailerService{}})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	service.Start()
	service.Stop()

	err = service.Enqueue(createTestMail("after-stop"))
	if !errors.Is(err, ErrServiceStopped) {
		t.Fatalf("expected ErrServiceStopped, got %v", err)
	}
}

func TestMailService_EnqueueContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	// WorkerCount 1, queue size 1, never started, so the queue fills and the
	// next Enqueue blocks until the context is cancelled.
	service, err := NewMailService(&MailServiceConfig{Ctx: ctx, WorkerCount: 1, Mailer: &MockMailerService{}})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	if err := service.Enqueue(createTestMail("fills-buffer")); err != nil {
		t.Fatalf("first Enqueue failed: %v", err)
	}

	errCh := make(chan error, 1)
	go func() { errCh <- service.Enqueue(createTestMail("blocks")) }()

	// Give the goroutine time to block on the full queue, then cancel.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Enqueue did not unblock after context cancellation")
	}
}

func TestMailService_Timeout(t *testing.T) {
	var deadlineSeen atomic.Bool
	mockMailer := &MockMailerService{
		SendFunc: func(ctx context.Context, _ MailContent) error {
			if _, ok := ctx.Deadline(); ok {
				deadlineSeen.Store(true)
			}
			return nil
		},
	}

	service, err := NewMailService(&MailServiceConfig{
		WorkerCount: 1,
		Timeout:     time.Second,
		Mailer:      mockMailer,
	})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	service.Start()
	if err := service.Enqueue(createTestMail("with-timeout")); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}
	service.Stop()

	if !deadlineSeen.Load() {
		t.Error("expected Send to receive a context with a deadline")
	}
}

func TestMailService_ContextCancellationStopsWorkers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	mockMailer := &MockMailerService{
		SendFunc: func(ctx context.Context, _ MailContent) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}

	service, err := NewMailService(&MailServiceConfig{Ctx: ctx, WorkerCount: 2, Mailer: mockMailer})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	service.Start()

	_ = service.Enqueue(createTestMail("blocked"))
	cancel()

	done := make(chan struct{})
	go func() { service.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("workers did not exit after context cancellation")
	}
}

func TestMailService_ConcurrentEnqueue(t *testing.T) {
	mockMailer := &MockMailerService{}
	service, err := NewMailService(&MailServiceConfig{WorkerCount: 8, QueueSize: 128, Mailer: mockMailer})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	service.Start()

	const producers, perProducer = 10, 20
	var wg sync.WaitGroup
	for p := range producers {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			for i := range perProducer {
				if err := service.Enqueue(createTestMail(fmt.Sprintf("p%d-%d", p, i))); err != nil {
					t.Errorf("Enqueue failed: %v", err)
					return
				}
			}
		}(p)
	}
	wg.Wait()
	service.Stop()

	if want := producers * perProducer; mockMailer.Count() != want {
		t.Errorf("sent %d emails, want %d", mockMailer.Count(), want)
	}
}

// ExampleMailService_Enqueue demonstrates creating a service, enqueuing an
// email, and having it processed by a mock mailer.
func ExampleMailService_Enqueue() {
	mockMailer := &MockMailerService{}

	service, err := NewMailService(&MailServiceConfig{WorkerCount: 1, Mailer: mockMailer})
	if err != nil {
		fmt.Printf("error creating service: %v\n", err)
		return
	}

	service.Start()

	content, err := NewMailContentBuilder().
		WithFromName("Example Sender").
		WithFromAddress("sender@example.net").
		WithToName("Example Recipient").
		WithToAddress("recipient@example.net").
		WithMimeType(MimeTypeTextPlain).
		WithSubject("Example Subject").
		WithBody("This is the example email body.").
		Build()
	if err != nil {
		fmt.Printf("error building content: %v\n", err)
		return
	}

	if err := service.Enqueue(content); err != nil {
		fmt.Printf("error enqueuing email: %v\n", err)
	}

	// Stop drains the queue, guaranteeing the message is processed first.
	service.Stop()

	fmt.Printf("Mock mailer received %d email(s).\n", mockMailer.Count())
	// Output:
	// Mock mailer received 1 email(s).
}
