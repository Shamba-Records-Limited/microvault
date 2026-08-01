// Package auth authenticates the admin by proving control of a Stellar key,
// then issues a JWT session token. It follows the SEP-10 challenge-response
// shape: the server hands out a transaction to sign, and a valid signature
// stands in for a password.
//
// # The flow
//
// GenerateChallenge builds a Stellar transaction — sourced by the server account and
// carrying a ManageData "auth" operation sourced by the admin account with a random
// nonce, time-bounded, tagged with the challenge ID in the memo — signs it with the
// server key, and stores it (see ChallengeStore). The transaction is never submitted
// to the network; it exists only to be signed. The caller countersigns it with the
// admin private key and returns it to VerifySignedChallenge, which confirms the
// challenge still exists and is unexpired, that the signed transaction hashes to the
// original (signatures excluded), and that an admin signature is present. Challenges
// are single-use: they are deleted on success to block replay. A verified caller is
// then issued a token with JWTService.GenerateToken.
//
// # Key separation
//
// The challenge signing key (StellarConfig.ServerSecretKey) must differ from the admin
// key it authenticates, and both NewChallengeService and config loading reject a shared
// key. Because a challenge is handed out already signed, a shared key would let an
// unsigned echo of that challenge satisfy verification. Verification additionally skips
// any signature made by the server key before looking for the admin's.
//
// # Pieces
//
// ChallengeService (challenge.go) runs the challenge lifecycle. ChallengeStore
// (store.go) persists challenges with automatic expiry; RedisStore is the
// production implementation, backed by Redis TTLs. JWTService (jwt.go) mints and
// validates HMAC-SHA256 tokens whose Claims carry the admin public key. errors.go
// holds the sentinel errors handlers map onto HTTP status codes.
package auth
