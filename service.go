package mailer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// ErrServiceStopped is returned by Enqueue when the service has been stopped.
var ErrServiceStopped = errors.New("mailer service is stopped")

const (
	// ValidMaxWorkerCount is the maximum number of concurrent workers allowed.
	ValidMaxWorkerCount = 100
	// ValidMinWorkerCount is the minimum number of concurrent workers allowed.
	ValidMinWorkerCount = 1
)

// MailQueueError is a configuration/validation error for the queue service.
type MailQueueError struct {
	Message string
}

func (e *MailQueueError) Error() string { return e.Message }

// MailQueueService is the public contract implemented by MailService. It lets
// callers depend on the queueing behaviour without binding to the concrete type.
type MailQueueService interface {
	Enqueue(content MailContent) error
}

// MailServiceConfig configures a MailService.
type MailServiceConfig struct {
	// Ctx is the base context that governs the lifetime of the workers.
	// When it is cancelled every worker stops as soon as it observes the
	// cancellation. If nil, context.Background() is used.
	Ctx context.Context

	// WorkerCount is the number of concurrent workers that process the queue.
	// It must be between ValidMinWorkerCount and ValidMaxWorkerCount.
	WorkerCount int

	// QueueSize is the capacity of the internal buffered queue. When zero it
	// defaults to WorkerCount. A larger queue absorbs bursts before Enqueue
	// starts to block (back-pressure).
	QueueSize int

	// Timeout, when greater than zero, bounds every individual Send call with a
	// per-message deadline derived from the worker context.
	Timeout time.Duration

	// Mailer is the transport responsible for delivering emails. Must not be nil.
	Mailer MailerService
}

// MailService is a concurrent, queue-backed email dispatcher. Callers Enqueue
// validated MailContent and a pool of workers delivers it through the configured
// MailerService. It is safe for concurrent use by multiple goroutines.
type MailService struct {
	ctx         context.Context
	workerCount int
	timeout     time.Duration
	content     chan MailContent
	mailer      MailerService
	wg          sync.WaitGroup

	// mu guards the stopped flag and, critically, serialises the close of the
	// content channel against in-flight sends. Enqueue takes the read lock for
	// the duration of a send so that Stop (which takes the write lock) can never
	// close the channel while a send is in progress. This prevents both the
	// "send on closed channel" panic and the previous dead-lock where a blocked
	// Enqueue held an exclusive lock that Start/Stop needed.
	mu       sync.RWMutex
	stopped  bool
	started  atomic.Bool
	stopOnce sync.Once
}

// NewMailService validates the configuration and returns a ready-to-start service.
func NewMailService(conf *MailServiceConfig) (*MailService, error) {
	if conf == nil {
		return nil, &MailQueueError{Message: "MailServiceConfig cannot be nil"}
	}

	if conf.Mailer == nil {
		return nil, &MailQueueError{Message: "Mailer cannot be nil"}
	}

	if conf.WorkerCount < ValidMinWorkerCount || conf.WorkerCount > ValidMaxWorkerCount {
		return nil, &MailQueueError{Message: fmt.Sprintf("WorkerCount must be between %d and %d", ValidMinWorkerCount, ValidMaxWorkerCount)}
	}

	queueSize := conf.QueueSize
	if queueSize <= 0 {
		queueSize = conf.WorkerCount
	}

	internalCtx := conf.Ctx
	if internalCtx == nil {
		internalCtx = context.Background()
	}

	return &MailService{
		ctx:         internalCtx,
		workerCount: conf.WorkerCount,
		timeout:     conf.Timeout,
		content:     make(chan MailContent, queueSize),
		mailer:      conf.Mailer,
	}, nil
}

// Start launches the worker goroutines. It is idempotent: calling it more than
// once has no additional effect.
func (ref *MailService) Start() {
	if !ref.started.CompareAndSwap(false, true) {
		return
	}

	for i := 1; i <= ref.workerCount; i++ {
		ref.wg.Add(1)
		go ref.worker(i)
	}
}

func (ref *MailService) worker(workerNum int) {
	defer ref.wg.Done()
	slog.Debug("worker started", "worker", workerNum)

	for {
		select {
		case <-ref.ctx.Done():
			slog.Debug("context done, worker stopping", "worker", workerNum)
			return
		case content, ok := <-ref.content:
			if !ok {
				slog.Debug("queue drained, worker stopping", "worker", workerNum)
				return
			}
			ref.deliver(workerNum, content)
		}
	}
}

func (ref *MailService) deliver(workerNum int, content MailContent) {
	sendCtx := ref.ctx
	if ref.timeout > 0 {
		var cancel context.CancelFunc
		sendCtx, cancel = context.WithTimeout(ref.ctx, ref.timeout)
		defer cancel()
	}

	slog.Debug("sending email", "worker", workerNum)
	if err := ref.mailer.Send(sendCtx, content); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			slog.Warn("email sending cancelled by context", "worker", workerNum, "error", err)
		} else {
			slog.Error("error sending email", "worker", workerNum, "error", err)
		}
	}
}

// Stop stops accepting new work, closes the queue, and waits for the workers to
// drain any already-queued messages. It is safe to call multiple times and from
// multiple goroutines.
func (ref *MailService) Stop() {
	ref.stopOnce.Do(func() {
		ref.mu.Lock()
		ref.stopped = true
		close(ref.content)
		ref.mu.Unlock()

		slog.Debug("mailer service stopping, waiting for workers to drain the queue")
		ref.wg.Wait()
		slog.Debug("mailer service stopped, all workers finished")
	})
}

// Wait blocks until every worker goroutine has exited. Unlike Stop it does not
// close the queue; it is useful when workers are expected to exit because the
// context was cancelled.
func (ref *MailService) Wait() {
	ref.wg.Wait()
}

// Enqueue adds a message to the queue. It blocks while the queue is full
// (back-pressure) and returns an error if the service has been stopped or the
// context is cancelled before the message can be queued.
func (ref *MailService) Enqueue(content MailContent) error {
	ref.mu.RLock()
	defer ref.mu.RUnlock()

	if ref.stopped {
		slog.Debug("failed to enqueue email, service stopped")
		return ErrServiceStopped
	}

	select {
	case <-ref.ctx.Done():
		slog.Debug("failed to enqueue email, context cancelled", "error", ref.ctx.Err())
		return ref.ctx.Err()
	case ref.content <- content:
		slog.Debug("email enqueued successfully")
		return nil
	}
}
