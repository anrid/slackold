# Whut?

Delete your old Slack messages and files from a workspace.

# Build

```bash
docker build -t slackold .
```

# Run

```bash
# Delete messages posted by `massamun` before May 1, 2020, in any private, mpdm or dm channel
# involving coworker2 or teammate1:
docker run --rm slackold -me massamun -filter teammate1,coworker2 -before 20200501 -token oxop-xxx
```
