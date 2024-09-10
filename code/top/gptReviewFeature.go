package top

import (
	"fmt"
	"log"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/jrenjq/MiniChatSentryBot/repo"
	"github.com/jrenjq/MiniChatSentryBot/structs"
	"github.com/jrenjq/MiniChatSentryBot/utils"
)

func Gpt_review_feature(
	telegram_bot_updates *structs.Update_response,
	api_env_pathname string,
	uri_env_pathname string,
	deleted_messages_log_pathname string,
	system_prompt_pathname string,
	is_logged bool,
) {
	// get config setting(s) for this feature.
	debug_mode, feature_is_on := repo.Get_gpt_review_config_values_from_env_file(
		".conf",
		"GPT_REVIEW_FEATURE_DEBUG_MODE",
		"GPT_REVIEW_FEATURE_ON",
	)
	// configuration file variable decides whether to run this feature or not.
	if feature_is_on {
		// load Claude API key and system prompt string.
		claude_api_uri, claude_api_key, claude_system_prompt := repo.Get_claude_info(
			api_env_pathname,
			system_prompt_pathname,
			debug_mode,
		)

		// extract messages from the monolith Update object.
		messages_slice_ptr := repo.Get_msgs_from_updates(telegram_bot_updates, is_logged)
		if is_logged {
			log.Printf("MESSAGES FOR CLAUDE: %+v\n\n",
				strings.ReplaceAll(fmt.Sprintf("%+v", messages_slice_ptr), "\n", ""))
		}

		// remove previously deleted messages from being sent to Claude...
		// this is because updates is simply what the bot has seen in the past 24 hours...
		// and already deleted messages will still appear as an 'update'.
		previously_deleted_messages := repo.Get_string_slice_from_file(deleted_messages_log_pathname, "\n")
		remove_prev_deleted_msgs_from_message_slice(messages_slice_ptr, &previously_deleted_messages, debug_mode)

		// ask Claude LLM to rate the message on scam likelihood and inappropriateness on 1-10.
		// if it is greater than or equal (gte) to 6, add the message to a naughty list.
		message_naughty_list := []structs.MessageObject{}
		for _, message := range *messages_slice_ptr {
			var message_text string
			if message.Text != "" {
				message_text = message.Text
			} else if message.Caption != "" {
				message_text = message.Caption
			} else {
				// getting here means the message slice has malformed objects & prev functions failed at their jobs.
				log.Panicf("Error: no message text nor image caption found in message: %+v\n\n",
					strings.ReplaceAll(fmt.Sprintf("%+v", message), "\n", ""))
			}
			// get Claude to rate the message, based on system prompt.
			claude_response_text := get_text_response_from_claude(
				claude_api_uri,
				claude_api_key,
				claude_system_prompt,
				message_text,
				10,
				debug_mode,
			)
			if debug_mode {
				log.Printf("CLAUDE RECEIVED: %s\nCLAUDE SAYS: %s\n\n", message.Text, claude_response_text)
			}
			// parse Claude rating text for the message into a rating struct.
			claude_ratings := parse_claude_rating_string_into_struct(claude_response_text)
			if debug_mode {
				log.Printf("MESSAGE PARSED: %+v\n\n", claude_ratings)
			}
			// messages with ratings on either scam or inappropriateness greater than or equal (gte) ...
			// ... to specified ratings go into the naughty list.
			add_to_naughty_list_messages_rated_gte(6, 6, message, claude_ratings, &message_naughty_list)
		}
		if is_logged {
			log.Printf("NAUGHTY LIST: %+v\n\n",
				strings.ReplaceAll(fmt.Sprintf("%+v", message_naughty_list), "\n", ""))
		}

		// delete naughty messages.
		deleted_messages_ids := []structs.Telegram_message_id{}
		for _, naughty_message := range message_naughty_list {
			isDeleted := repo.Delete_one_message(uri_env_pathname, &naughty_message, debug_mode)
			if !isDeleted {
				if is_logged {
					log.Printf("[!] MESSAGE COULD NOT BE DELETED: %+v\n\n",
						strings.ReplaceAll(fmt.Sprintf("%+v", naughty_message), "\n", ""))
				}
			} else {
				deleted_messages_ids = append(deleted_messages_ids, structs.Telegram_message_id{
					Message_id: naughty_message.MessageId,
					Chat_id:    naughty_message.Chat.ID,
				})
			}
		}
		if is_logged {
			log.Printf("DELETED NAUGHTY MESSAGES IDS: %+v\n\n",
				strings.ReplaceAll(fmt.Sprintf("%+v", deleted_messages_ids), "\n", ""))
			log.Printf("DELETED/MARKED-FOR_DELETION: %d/%d\n\n",
				len(deleted_messages_ids), len(message_naughty_list))
		}
	}
}

// Helper function: send claude an input, get a response string from Claude in return.
func get_text_response_from_claude(
	claude_api_uri string,
	claude_api_key string,
	claude_system_prompt string,
	claude_query string,
	timeout_seconds int,
	debug_mode bool,
) string {
	// send message to Claude with its API key and system prompt, get a response.
	claude_response_ptr := new(structs.ClaudeResponse) // must use pointer to address (comply with JSON Decode).
	utils.Send_to_claude_one_message(
		claude_response_ptr,
		claude_api_uri,
		claude_api_key,
		claude_system_prompt,
		claude_query,
		timeout_seconds,
		debug_mode,
	)
	if debug_mode {
		log.Printf("CLAUDE RESPONSE: %+v\n", claude_response_ptr)
	}
	return claude_response_ptr.Content[0].Text // only one item in Content expected from Claude API.
}

// Helper function: returns a struct of parsed values from Claude rating response.
func parse_claude_rating_string_into_struct(claude_rating_string string) structs.ClaudeMessageRating {
	// Define the regular expression to match content within brackets
	re := regexp.MustCompile(`\((.*?)\)`)
	// Find all matches
	matches := re.FindAllStringSubmatch(claude_rating_string, -1)
	// Extract the captured groups; assumption is that (A)(B):(X)(Y) format will be adhered to.
	// A is rating for scam, B is rating for inappropriateness, ...
	// ... X is a 10 or less words explanation for A, and Y is a 10 or less word explanation for B.
	scam_rating_int, _ := strconv.Atoi(matches[0][1])
	inappropriate_rating_int, _ := strconv.Atoi(matches[1][1])
	scam_rating_reason := matches[2][1]
	inappropriate_rating_reason := matches[3][1]
	return structs.ClaudeMessageRating{
		ScamRatingInt:             scam_rating_int,
		ScamRatingReason:          scam_rating_reason,
		InappropriateRatingInt:    inappropriate_rating_int,
		InappropriateRatingReason: inappropriate_rating_reason,
	}
}

// Helper function: adds messages with rating greater than or equal (gte) X to the naughty list.
func add_to_naughty_list_messages_rated_gte(
	scam_rating_gte int,
	inappropriate_rating_gte int,
	message structs.MessageObject,
	message_rating structs.ClaudeMessageRating,
	naughty_messages_list_ptr *[]structs.MessageObject,
) {
	if (message_rating.ScamRatingInt >= scam_rating_gte) ||
		(message_rating.InappropriateRatingInt >= inappropriate_rating_gte) {
		// Add that message to the naughty list.
		*naughty_messages_list_ptr = append(*naughty_messages_list_ptr, message)
	}
}

// Helper function: removes previously deleted messages from message slice.
func remove_prev_deleted_msgs_from_message_slice(
	messages_slice_ptr *[]structs.MessageObject,
	previously_deleted_messages *[]string,
	debug_mode bool,
) {
	// make a set of previously deleted messages' unique identifiers (uids).
	// from: https://stackoverflow.com/a/13520159
	del_msgs_set := make(map[string]struct{}) // the value struct{} is nothing and consumes no space.
	for _, del_msg := range *previously_deleted_messages {
		del_msgs_set[del_msg] = struct{}{}
	}

	// get indices to remove from message slice.
	indices_to_remove := []int{}
	for index, message := range *messages_slice_ptr {
		message_uid := message.Get_uid_string()
		_, isPresent := del_msgs_set[message_uid]
		if isPresent {
			indices_to_remove = append(indices_to_remove, index)
		}
	}
	if debug_mode {
		log.Printf("Removing indices %+v from 'MESSAGES FOR CLAUDE'!\n\n", indices_to_remove)
	}

	// based on indices, remove the messages
	// note: removing message on index itself causes indexes to change. so remove from highest index.
	for index := len(indices_to_remove) - 1; index >= 0; index-- {
		// delete indices be like:
		// [   A B C D    ]
		//    0 1 2 3 4
		*messages_slice_ptr = slices.Delete(*messages_slice_ptr, index, index+1)
		if debug_mode {
			// note *p[0] -> index the pointer; (*p)[0] -> index the thing deferenced from pointer. (C nostalgia).
			log.Printf("REMOVE ALREADY DELETED MESSAGE[%d]: %+v\n\n", index, (*messages_slice_ptr)[index])
		}
	}
}
