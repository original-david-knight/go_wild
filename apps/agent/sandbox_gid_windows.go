package main

// getDockerGID falls back to the Docker group name on Windows because
// Windows file metadata does not expose Unix group IDs.
func getDockerGID() string {
	return "docker"
}
