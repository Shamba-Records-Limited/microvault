// Package auth authenticates the admin by proving control of a Stellar key,
// then issues a JWT session token. It follows the SEP-10 challenge-response
// shape: the server hands out a transaction to sign, and a valid signature
// stands in for a password.
//
// # The flow
//
// GenerateChallenge builds a Stellar transaction — a ManageData "auth" operation
// carrying a random nonce, sourced by the admin account, time-bounded, and tagged
// with the challenge ID in the memo — signs it with the server key, and stores it
// (see ChallengeStore). The transaction is never submitted to the network; it
// exists only to be signed. The caller signs it with the admin private key and
// returns it to VerifySignedChallenge, which confirms the challenge still exists
// and is unexpired, that the signed transaction hashes to the original (signatures
// excluded), and that the admin signature is present. Challenges are single-use:
// they are deleted on success to block replay. A verified caller is then issued a
// token with JWTService.GenerateToken.
//
// # Pieces
//
// ChallengeService (challenge.go) runs the challenge lifecycle. ChallengeStore
// (store.go) persists challenges with automatic expiry; RedisStore is the
// production implementation, backed by Redis TTLs. JWTService (jwt.go) mints and
// validates HMAC-SHA256 tokens whose Claims carry the admin public key. errors.go
// holds the sentinel errors handlers map onto HTTP status codes.
package auth
