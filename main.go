package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/slack-go/slack"
	"github.com/spf13/pflag"
)

func main() {
	// Slack OAuth Access Token beginning with `xoxp-`.
	// To get a token you'll need to create a new Slack app and add
	// it to your workspace:
	// https://api.slack.com/apps
	//
	// You'll also need to give your app the correct scopes (permissions):
	// channels:history - View messages and other content in a user’s public channels
	// channels:read - View basic information about public channels in a workspace
	// chat:write - Send messages on a user’s behalf
	// files:read - View files shared in channels and conversations that a user has access to
	// files:write - Upload, edit, and delete files on a user’s behalf
	// groups:history - View messages and other content in a user’s private channels
	// groups:read - View basic information about a user’s private channels
	// im:history - View messages and other content in a user’s direct messages
	// im:read - View basic information about a user’s direct messages
	// mpim:history - View messages and other content in a user’s group direct messages
	// mpim:read - View basic information about a user’s group direct messages
	// users:read - View people in a workspace
	//
	token := pflag.String("token", "", "Slack token")

	// Your Slack username.
	me := pflag.String("me", "", "My Slack username")

	// Filter channels, multi-party and direct message channels using this pattern.
	filter := pflag.String("filter", "", "Filter channels, multi-party and direct message channels using this pattern, e.g. 'ace,base' becomes regexp /(ace|base)/i")

	// Delete message before the given date.
	before := pflag.String("before", "", "Delete messages before this date (format: YYYYMMDD)")

	commit := pflag.Bool("commit", false, "Perform the actual delete operation")

	pflag.Parse()

	var myUserID string

	// Try loading token from env var if no --token flag was passed.
	if *token == "" {
		*token = os.Getenv("MY_SLACK_TOKEN")
		if *token == "" {
			panic("env MY_SLACK_TOKEN missing and --token flag empty")
		}
	}

	if *me == "" {
		panic("--me flag missing")
	}
	fmt.Printf("Me: %s\n", *me)

	// Create filter regexp.
	var pattern *regexp.Regexp
	if *filter != "" {
		pattern = regexp.MustCompile(`(?i)(` + strings.ReplaceAll(*filter, ",", "|") + `)`)
		fmt.Printf("Filter pattern: %s\n", pattern.String())
	}

	// Convert --before time string (YYYYMMDD format) to a Slack timestamp.
	var beforeTS string
	var beforeTSUnix slack.JSONTime

	if *before != "" {
		t, err := time.Parse("20060102", *before)
		if err != nil {
			panic(err)
		}

		beforeTS = toSlackTimestamp(t)
		beforeTSUnix = slack.JSONTime(t.Unix())

		fmt.Printf("Before: %s Slack TS: %s (sanity check: %s)\n", t.String(), beforeTS, fromSlackTimestamp(beforeTS))
	}

	fmt.Printf("Using token: %s\n", *token)

	api := slack.New(*token)

	// Fetch users.
	users := make(map[string]string)

	{
		fmt.Println("Fetching users ..")

		res, err := api.GetUsers()
		if err != nil {
			panic(err)
		}

		for _, u := range res {
			if u.Name == *me {
				myUserID = u.ID
			}
			users[u.ID] = u.Name
		}

		if myUserID == "" {
			panic("could not find user: " + *me)
		}

		fmt.Printf("Found myself: ID: %s, Name: %s\n", myUserID, users[myUserID])
	}

	// Fetch files.
	var filesToDelete []slack.File
	{
		var found int

		p := &slack.ListFilesParameters{
			User: myUserID,
		}

		for {
			var res []slack.File
			var err error

			res, p, err = api.ListFiles(*p)
			if err != nil {
				panic(err)
			}

			for _, f := range res {
				if f.Created < beforeTSUnix {
					found++
					fmt.Printf("%03d. Found file: %s (created: %s)\n", found, f.Name, f.Created.Time().String())
					filesToDelete = append(filesToDelete, f)
				}
			}

			if p.Cursor == "" {
				break
			}

			fmt.Printf("Next cursor: %s (files: %d)\n", p.Cursor, len(filesToDelete))
		}

		fmt.Printf("Found %d files.\n", len(filesToDelete))
	}

	// Fetch channels.
	var channels []slack.Channel
	{
		var found int
		var cursor string

		for {
			res, next, err := api.GetConversations(&slack.GetConversationsParameters{
				Types:  []string{"private_channel", "mpim", "im"},
				Cursor: cursor,
			})
			if err != nil {
				panic(err)
			}

			for _, c := range res {
				if c.IsPrivate || c.IsMpIM {
					if pattern.MatchString(c.Name) {
						found++
						fmt.Printf("%03d. CH ID: %s, Name: %s\n", found, c.ID, c.Name)
						channels = append(channels, c)
					}
				}
				if c.IsIM {
					username := users[c.User]
					if pattern.MatchString(username) {
						found++
						fmt.Printf("%03d. IM ID: %s, Name: %s\n", found, c.ID, username)
						channels = append(channels, c)
					}
				}
			}

			if next == "" {
				break
			}

			fmt.Printf("Next cursor: %s (channels: %d)\n", next, len(channels))
			cursor = next
		}

		fmt.Printf("Found %d channels.\n", len(channels))
	}

	// Fetch messages.
	var messagesToDelete []slack.Message
	{
		var found int

		for _, c := range channels {

			var cursor string
			for {
				res, err := api.GetConversationHistory(&slack.GetConversationHistoryParameters{
					ChannelID: c.ID,
					Cursor:    cursor,
				})
				if err != nil {
					panic(err)
				}

				for _, m := range res.Messages {
					if m.User == myUserID {
						if beforeTS != "" && m.Timestamp > beforeTS {
							continue
						}

						found++
						fmt.Printf("%03d. MSG TS: %s, Text: %s\n", found, fromSlackTimestamp(m.Timestamp), m.Text)

						m.Channel = c.ID
						messagesToDelete = append(messagesToDelete, m)
					}
				}

				if !res.HasMore {
					break
				}

				cursor = res.ResponseMetaData.NextCursor
				fmt.Printf("Next cursor: %s (messages: %d)\n", cursor, len(res.Messages))
			}
		}
	}

	fmt.Printf("\nFound %d messages and %d files to delete!\n\n", len(messagesToDelete), len(filesToDelete))

	if !*commit {
		fmt.Printf("Run command again with --commit flag to perform the delete operation!\n\n")
		os.Exit(0)
	}

	// Delete messages.
	{
		for i, m := range messagesToDelete {
			for {
				ch, ts, err := api.DeleteMessage(m.Channel, m.Timestamp)
				if err != nil {
					if !strings.Contains(err.Error(), "rate limit") {
						fmt.Printf("Delete failed: %s (message: ID: %s, Text: %s)\n", err.Error(), m.Timestamp, m.Text)
						break
					}

					// We were rate limited, wait a sec and retry!
					fmt.Printf("Rate limited, retrying in 1 sec ..\n")
					time.Sleep(1000 * time.Millisecond)
					continue
				}

				fmt.Printf("%03d. Deleted message: %s %s\n", i+1, ch, ts)
				break
			}

			time.Sleep(200 * time.Millisecond)
		}
	}

	// Delete files.
	{
		for i, f := range filesToDelete {
			for {
				err := api.DeleteFile(f.ID)
				if err != nil {
					if !strings.Contains(err.Error(), "rate limit") {
						fmt.Printf("Delete failed: %s (file: ID: %s, Name: %s)\n", err.Error(), f.ID, f.Name)
						break
					}

					// We were rate limited, wait a sec and retry!
					fmt.Printf("Rate limited, retrying in 1 sec ..\n")
					time.Sleep(1000 * time.Millisecond)
					continue
				}

				fmt.Printf("%03d. Deleted file: %s (created: %s)\n", i+1, f.Name, f.Created.Time().String())
				break
			}

			time.Sleep(200 * time.Millisecond)
		}
	}

	fmt.Printf("\nIt's a Done Deal!\n\n")
}

func fromSlackTimestamp(ts string) time.Time {
	parts := strings.Split(ts, ".")
	sec, _ := strconv.ParseInt(parts[0], 10, 64)
	nsec, _ := strconv.ParseInt(parts[1], 10, 64)
	return time.Unix(sec, nsec*1000)
}

func toSlackTimestamp(t time.Time) string {
	ts := fmt.Sprintf("%d", t.UnixNano()/1000)
	return fmt.Sprintf("%s.%s", ts[0:len(ts)-6], ts[len(ts)-6:])
}
