// Package jobs is the background-work layer, wrapping the Redis-backed asynq
// library behind a single Scheduler. It covers the three roles asynq separates:
// enqueuing tasks, running them on a worker pool, and firing recurring cron jobs.
//
// A task is a type string plus a JSON payload. NewTask marshals a value into one
// and ParsePayload unmarshals it back inside a handler, so producers and
// consumers agree on the payload shape without sharing more than the type name.
//
// # Lifecycle
//
// Build a Scheduler with NewScheduler from a Config (DefaultConfig supplies sane
// values — concurrency, the priority queues, UTC). Wire each task type to its
// handler with RegisterHandler, and register recurring work with RegisterCronJob.
// Enqueue runs a task as soon as a worker is free; Schedule runs one at a future
// time. StartWorker runs the pool that processes tasks and StartScheduler runs
// the cron loop — typically as separate long-lived goroutines — and Shutdown
// stops both and closes the client cleanly.
//
// # Queues and retries
//
// Work is spread across three priority queues — critical, default, and low —
// drained in that order of weight; the CriticalQueue, DefaultQueue, and LowQueue
// options pick one when enqueuing. Failed tasks are retried with a backoff that
// grows by a minute per attempt, and the MaxRetry3, MaxRetry5, and MaxRetry10
// options cap how many times.
package jobs
