package jobs

import (
	"context"
	"log"
	"time"

	"github.com/hibiken/asynq"
)

// Scheduler manages job scheduling and execution
type Scheduler struct {
	client    *asynq.Client
	scheduler *asynq.Scheduler
	server    *asynq.Server
	mux       *asynq.ServeMux
}

// Config holds scheduler configuration
type Config struct {
	RedisAddr     string
	RedisPassword string
	RedisDB       int
	Concurrency   int
	Queues        map[string]int
	Location      *time.Location
}

// DefaultConfig returns default scheduler configuration
func DefaultConfig(redisAddr, redisPassword string, redisDB int) *Config {
	return &Config{
		RedisAddr:     redisAddr,
		RedisPassword: redisPassword,
		RedisDB:       redisDB,
		Concurrency:   10,
		Queues: map[string]int{
			"critical": 6,
			"default":  3,
			"low":      1,
		},
		Location: time.UTC,
	}
}

// NewScheduler creates a new job scheduler
func NewScheduler(cfg *Config) *Scheduler {
	redisOpt := asynq.RedisClientOpt{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	}

	client := asynq.NewClient(redisOpt)

	scheduler := asynq.NewScheduler(
		redisOpt,
		&asynq.SchedulerOpts{
			Location: cfg.Location,
		},
	)

	server := asynq.NewServer(
		redisOpt,
		asynq.Config{
			Concurrency: cfg.Concurrency,
			Queues:      cfg.Queues,
			RetryDelayFunc: func(n int, e error, t *asynq.Task) time.Duration {
				return time.Duration(n) * time.Minute
			},
		},
	)

	return &Scheduler{
		client:    client,
		scheduler: scheduler,
		server:    server,
		mux:       asynq.NewServeMux(),
	}
}

// RegisterHandler registers a task handler
func (s *Scheduler) RegisterHandler(taskType string, handler func(*asynq.Task) error) {
	s.mux.HandleFunc(taskType, func(ctx context.Context, t *asynq.Task) error {
		return handler(t)
	})
}

// RegisterCronJob registers a recurring cron job
func (s *Scheduler) RegisterCronJob(cronSpec string, taskType string, payload []byte, opts ...asynq.Option) error {
	task := asynq.NewTask(taskType, payload)
	_, err := s.scheduler.Register(cronSpec, task, opts...)
	if err != nil {
		return err
	}
	log.Printf("Registered cron job: %s with schedule: %s", taskType, cronSpec)
	return nil
}

// Enqueue enqueues a task for immediate processing
func (s *Scheduler) Enqueue(taskType string, payload []byte, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	task := asynq.NewTask(taskType, payload)
	return s.client.Enqueue(task, opts...)
}

// Schedule schedules a task for future processing
func (s *Scheduler) Schedule(taskType string, payload []byte, processAt time.Time, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	task := asynq.NewTask(taskType, payload)
	opts = append(opts, asynq.ProcessAt(processAt))
	return s.client.Enqueue(task, opts...)
}

// StartWorker starts the worker to process tasks
func (s *Scheduler) StartWorker() error {
	log.Println("Starting job worker...")
	return s.server.Run(s.mux)
}

// StartScheduler starts the scheduler for cron jobs
func (s *Scheduler) StartScheduler() error {
	log.Println("Starting job scheduler...")
	return s.scheduler.Run()
}

// Shutdown gracefully shuts down the scheduler and worker
func (s *Scheduler) Shutdown() {
	log.Println("Shutting down scheduler...")
	s.scheduler.Shutdown()
	s.server.Shutdown()
	if err := s.client.Close(); err != nil {
		log.Printf("Error closing client: %v", err)
	}
}

// QueueOptions for different priority levels
var (
	CriticalQueue = asynq.Queue("critical")
	DefaultQueue  = asynq.Queue("default")
	LowQueue      = asynq.Queue("low")
)

// Common retry configurations
var (
	MaxRetry3  = asynq.MaxRetry(3)
	MaxRetry5  = asynq.MaxRetry(5)
	MaxRetry10 = asynq.MaxRetry(10)
)
