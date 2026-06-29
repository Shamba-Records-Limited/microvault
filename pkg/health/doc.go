// Package health exposes liveness and readiness probes as Fiber handlers, in the
// shape an orchestrator like Kubernetes expects.
//
// Checker holds the dependencies a readiness check needs and answers two
// questions. LivenessProbe reports whether the process is alive — it always
// succeeds while the app is running, so a failure means the process should be
// restarted. ReadinessProbe reports whether the app can serve traffic, by
// confirming its critical dependencies are reachable: the database, the cache,
// and, when configured, the Stellar RPC. The Stellar check counts as healthy only
// if both the network info and the latest ledger can be fetched, and the whole
// readiness check runs under a short timeout so a hung dependency fails fast
// rather than blocking the probe.
//
// Stellar is optional. Build a Checker with NewChecker to include the Stellar
// probe, or NewCheckerWithoutStellar to skip it. HealthHandler backs the liveness
// endpoint and ReadyHandler the readiness endpoint; on failure ReadyHandler
// returns a per-dependency breakdown so an unready instance is easy to diagnose.
package health
