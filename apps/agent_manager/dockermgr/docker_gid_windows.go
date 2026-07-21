package dockermgr

// Windows file metadata does not expose Unix group IDs.
func getDockerGID() string {
	return "docker"
}
