// Package gitlabtracker implements the RimeDeck-side surface for the GitLab
// Issue Tracker integration. It owns:
//
//   - URL parsing and host allowlisting (urls.go)
//   - Versioned AES-256-GCM crypto for tokens and webhook secrets
//   - An SSRF-safe HTTP transport for outbound REST calls
//   - The minimal GitLab REST client used by the validator and importer
//
// Nothing in this package writes plaintext credentials to disk or logs; the
// only permitted egress for tokens is the encrypted BYTEA column referenced
// by gitlab_tracker_connection.
package gitlabtracker
