# Whut?

Delete your old Slack messages and files from a workspace.

# Setup Slack

1. Create a new Slack app.

2. Create a new OAuth Access Token for the app:

   <img src="./slack-app-oauth.png" height="500" />

3. Add the following User Token scopes:

   - channels:history - View messages and other content in a user’s public channels
   - channels:read - View basic information about public channels in a workspace
   - chat:write - Send messages on a user’s behalf
   - files:read - View files shared in channels and conversations that a user has access to
   - files:write - Upload, edit, and delete files on a user’s behalf
   - groups:history - View messages and other content in a user’s private channels
   - groups:read - View basic information about a user’s private channels
   - im:history - View messages and other content in a user’s direct messages
   - im:read - View basic information about a user’s direct messages
   - mpim:history - View messages and other content in a user’s group direct messages
   - mpim:read - View basic information about a user’s group direct messages
   - users:read - View people in a workspace

# Run

```bash
# Delete messages posted by `massamun` before May 1, 2020, in any private, mpdm or dm channel
# involving coworker2 or teammate1.
#
# NOTE: It's a dry-run, nothing is actually deleted unless you also pass the --commit flag.
#
docker run --rm anrid/slackold --me massamun --filter teammate1,coworker2 --before 20200501 --token xoxp-...
```

# Build

```bash
docker build -t slackold -t anrid/slackold:latest .
```
