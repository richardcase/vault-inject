package webhook

import "time"

// Parameters configures parameters for the webhook.
type Parameters struct {
	// ConfigFile is the path to the sidecar injection configuration file.
	//ConfigFile string

	// MeshFile is the path to the mesh configuration file.
	//MeshFile string

	// CertFile is the path to the x509 certificate for https.
	CertFile string

	// KeyFile is the path to the x509 private key matching `CertFile`.
	KeyFile string

	// Port is the webhook port, e.g. typically 443 for https.
	Port int

	// HealthCheckInterval configures how frequently the health check
	// file is updated. Value of zero disables the health check
	// update.
	HealthCheckInterval time.Duration

	// HealthCheckFile specifies the path to the health check file
	// that is periodically updated.
	HealthCheckFile string
}
