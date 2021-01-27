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

	// Get all users in workspace.
	users, myUserID := getUsers(api, *me)

	// Get all files.
	files := getFiles(api, myUserID, beforeTSUnix)

	// Get channels.
	channels := getChannels(api, pattern, users)

	// Get messages.
	messages := getMessages(api, channels, myUserID, beforeTS)

	fmt.Printf("\nFound %d messages and %d files to delete!\n\n", len(messages), len(files))

	if !*commit {
		fmt.Printf("Run command again with --commit flag to perform the delete operation!\n\n")
		os.Exit(0)
	}

	// Delete messages.
	deleteMessages(api, messages)

	// Delete files.
	deleteFiles(api, files)

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

func getUsers(api *slack.Client, me string) (users map[string]string, myUserID string) {
	users = make(map[string]string)

	fmt.Println("Loading all users in workspace ..")

	res, err := api.GetUsers()
	if err != nil {
		panic(err)
	}

	for _, u := range res {
		if u.Name == me {
			myUserID = u.ID
		}
		users[u.ID] = u.Name
	}

	if myUserID == "" {
		panic("could not find user: " + me)
	}

	fmt.Printf("Found myself: ID: %s, Name: %s\n", myUserID, users[myUserID])
	return
}

func getFiles(api *slack.Client, myUserID string, before slack.JSONTime) (filesToDelete []slack.File) {
	var found int

	p := &slack.ListFilesParameters{
		User: myUserID,
	}

	for {
		var res []slack.File
		var err error

		res, p, err = api.ListFiles(*p)
		if err != nil {
			if !strings.Contains(err.Error(), "rate limit") {
				panic(err)
			}

			fmt.Printf("Rate limited, retrying in 1 sec ..\n")
			time.Sleep(1000 * time.Millisecond)
		}

		for _, f := range res {
			if f.Created < before {
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
	return
}

func getChannels(api *slack.Client, filter *regexp.Regexp, users map[string]string) (channels []slack.Channel) {
	var found int
	var cursor string

	for {
		res, next, err := api.GetConversations(&slack.GetConversationsParameters{
			Types:  []string{"private_channel", "mpim", "im"},
			Cursor: cursor,
		})
		if err != nil {
			if !strings.Contains(err.Error(), "rate limit") {
				panic(err)
			}

			fmt.Printf("Rate limited, retrying in 1 sec ..\n")
			time.Sleep(1000 * time.Millisecond)
			continue
		}

		for _, c := range res {
			if c.IsMpIM {
				// Include all MpIM channels if there's no filter.
				if filter == nil || filter.MatchString(c.Name) {
					found++
					fmt.Printf("%03d. MpIM ID: %s, Name: %s\n", found, c.ID, c.Name)
					channels = append(channels, c)
				}
			}
			if c.IsIM {
				username := users[c.User]
				// Include all DM channels if there's no filter.
				if filter == nil || filter.MatchString(username) {
					found++
					fmt.Printf("%03d. IM ID: %s, Name: %s\n", found, c.ID, username)
					channels = append(channels, c)
				}
			}
			if c.IsPrivate {
				// Skip all other private channels if there's no filter.
				if filter != nil && filter.MatchString(c.Name) {
					found++
					fmt.Printf("%03d. CH ID: %s, Name: %s\n", found, c.ID, c.Name)
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
	return
}

func getMessages(api *slack.Client, channels []slack.Channel, myUserID, beforeTS string) (messagesToDelete []slack.Message) {
	var found int

	for _, c := range channels {

		var cursor string
		var last int

		for {
			res, err := api.GetConversationHistory(&slack.GetConversationHistoryParameters{
				ChannelID: c.ID,
				Cursor:    cursor,
				Limit:     1000,
			})
			if err != nil {
				if !strings.Contains(err.Error(), "rate limit") {
					panic(err)
				}

				fmt.Printf("Rate limited, retrying in 1 sec ..\n")
				time.Sleep(1000 * time.Millisecond)
				continue
			}

			for _, m := range res.Messages {
				if m.User == myUserID {
					if beforeTS != "" && m.Timestamp > beforeTS {
						continue
					}

					if found == last {
						fmt.Println("")
					}

					found++

					channelName := c.Name
					if channelName == "" {
						channelName = c.ID
					}

					fmt.Printf("%03d. CH: %s MSG TS: %s, Text: %s\n", found, channelName, fromSlackTimestamp(m.Timestamp), m.Text)

					m.Channel = c.ID
					messagesToDelete = append(messagesToDelete, m)
				}
			}

			if !res.HasMore {
				break
			}

			cursor = res.ResponseMetaData.NextCursor

			if found == last {
				fmt.Printf(".")
			} else {
				fmt.Printf("Reading: %s\n", cursor)
			}
			last = found
		}
	}

	return
}

func deleteMessages(api *slack.Client, ms []slack.Message) {
	for i, m := range ms {

		for {
			ch, ts, err := api.DeleteMessage(m.Channel, m.Timestamp)
			if err != nil {
				if !strings.Contains(err.Error(), "rate limit") {
					fmt.Printf("Delete failed: %s (message: ID: %s, Text: %s)\n", err.Error(), m.Timestamp, m.Text)
					break
				}

				fmt.Printf("Rate limited, retrying in 1 sec ..\n")
				time.Sleep(1000 * time.Millisecond)
				continue
			}

			fmt.Printf("%03d. Deleted message: %s %s\n", i+1, ch, ts)
			break
		}

		time.Sleep(50 * time.Millisecond)
	}
}

func deleteFiles(api *slack.Client, fs []slack.File) {
	for i, f := range fs {
		for {
			err := api.DeleteFile(f.ID)
			if err != nil {
				if !strings.Contains(err.Error(), "rate limit") {
					fmt.Printf("Delete failed: %s (file: ID: %s, Name: %s)\n", err.Error(), f.ID, f.Name)
					break
				}

				fmt.Printf("Rate limited, retrying in 1 sec ..\n")
				time.Sleep(1000 * time.Millisecond)
				continue
			}

			fmt.Printf("%03d. Deleted file: %s (created: %s)\n", i+1, f.Name, f.Created.Time().String())
			break
		}

		time.Sleep(50 * time.Millisecond)
	}
}
