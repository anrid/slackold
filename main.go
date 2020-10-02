package main

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/slack-go/slack"
)

func main() {
	// Slack OAuth Access Token beginning with `oxop-`.
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
	token := flag.String("token", "", "Slack token")

	// Your Slack username.
	me := flag.String("me", "", "My Slack username")

	// Filter channels, multi-party and direct message channels using this pattern.
	filter := flag.String("filter", "", "Filter channels, multi-party and direct message channels using this pattern, e.g. 'ace,base' becomes regexp /(ace|base)/i")

	// Delete message before the given date.
	before := flag.String("before", "", "Delete messages before this date (format: YYYYMMDD)")

	commit := flag.Bool("commit", false, "Perform the actual delete operation")

	flag.Parse()

	var myUserID string

	// Try loading token from env var if no -token flag was passed.
	if *token == "" {
		*token = os.Getenv("MY_SLACK_TOKEN")
		if *token == "" {
			panic("env MY_SLACK_TOKEN missing and -token flag empty")
		}
	}

	if *me == "" {
		panic("-me flag missing")
	}
	fmt.Printf("Me: %s\n", *me)

	// Create filter regexp.
	var pattern *regexp.Regexp
	if *filter != "" {
		pattern = regexp.MustCompile(`(?i)(` + strings.ReplaceAll(*filter, ",", "|") + `)`)
		fmt.Printf("Filter pattern: %s\n", pattern.String())
	}

	// Convert -before time string to a Slack timestamp.
	var beforeTS string
	if *before != "" {
		t, err := time.Parse("20060102", *before)
		if err != nil {
			panic(err)
		}
		beforeTS = toSlackTimestamp(t)
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

	// Fetch channels.
	var channels []slack.Channel
	var cursor string

	for {
		res, next, err := api.GetConversations(&slack.GetConversationsParameters{
			Types:  []string{"private_channel", "mpim", "im"},
			Cursor: cursor,
		})
		if err != nil {
			panic(err)
		}

		for i, c := range res {
			if c.IsPrivate || c.IsMpIM {
				if pattern.MatchString(c.Name) {
					fmt.Printf("%03d. CH ID: %s, Name: %s\n", i+1, c.ID, c.Name)
					channels = append(channels, c)
				}
			}
			if c.IsIM {
				username := users[c.User]
				if pattern.MatchString(username) {
					fmt.Printf("%03d. IM ID: %s, Name: %s\n", i+1, c.ID, username)
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

	// Fetch messages.
	var toDelete []slack.Message
	{
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

				for i, m := range res.Messages {
					if m.User == myUserID {
						if beforeTS != "" && m.Timestamp > beforeTS {
							continue
						}
						fmt.Printf("%03d. MSG TS: %s, Text: %s\n", i+1, fromSlackTimestamp(m.Timestamp), m.Text)
						m.Channel = c.ID
						toDelete = append(toDelete, m)
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

	fmt.Printf("\nFound %d messages to delete!\n\n", len(toDelete))

	if !*commit {
		fmt.Printf("Run command again with -commit flag to perform the delete operation!\n\n")
		os.Exit(0)
	}

	// Delete messages.
	{
		for i, m := range toDelete {
			for {
				ch, ts, err := api.DeleteMessage(m.Channel, m.Timestamp)
				if err != nil {
					if !strings.Contains(err.Error(), "rate limit") {
						fmt.Printf("Delete failed: %s (message: ID: %s, Text: %s)\n", err.Error(), m.Timestamp, m.Text)
						break
					}
					// We were rate limited, wait a sec and retry!
					fmt.Printf("Rate limited, retrying in 1sec ..\n")
					time.Sleep(1000 * time.Millisecond)
					continue
				}

				fmt.Printf("%03d. Deleted message: %s %s\n", i+1, ch, ts)
				break
			}

			time.Sleep(200 * time.Millisecond)
		}
	}
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
