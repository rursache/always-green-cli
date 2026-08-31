package notify

import "fmt"

// TokenExpired warns that a workspace has dropped offline and cannot recover
// on its own. fromDesktop picks the message: a Slack-app workspace only gets
// here after an automatic re-read already failed.
func TokenExpired(workspace string, fromDesktop bool) {
	body := fmt.Sprintf("%s: Slack tokens expired. Run always-green reauth", workspace)
	if fromDesktop {
		body = fmt.Sprintf("%s: could not re-read the Slack app. Open Slack and sign in", workspace)
	}
	send("always-green", body)
}
