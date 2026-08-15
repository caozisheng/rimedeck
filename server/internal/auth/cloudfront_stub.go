package auth

// CloudFrontSigner is a stub for CloudFront URL signing used by the upstream
// Multica attachment download path. RimeDeck does not use CloudFront (zero
// cloud), so this is a no-op placeholder that lets test code compile.
// The production download handler checks for nil and falls back to proxy mode.
type CloudFrontSigner struct{}

// NewCloudFrontSignerFromEnv returns nil when the CloudFront env vars are not
// set, which is always the case in RimeDeck.
func NewCloudFrontSignerFromEnv() *CloudFrontSigner {
	return nil
}
