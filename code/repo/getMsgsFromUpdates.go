package repo

import (
	"log"

	"github.com/jrenjq/MiniChatSentryBot/structs"
)

/*
Reads the entire Telegram Bot API response JSON, and extracts as structs each message's message ids and texts.

parameters:
  - telegram_bot_updates *structs.Update_response: POINTER to the entire telegram bot update response.

returns:
  - *[]structs.MessageObject: POINTER to slice of MessageObjects that lives on the heap.
*/
func Get_msgs_from_updates(
	telegram_bot_updates *structs.Update_response,
) *[]structs.MessageObject {
	message_objects_ptr := new([]structs.MessageObject) // create a new slice of MessageOject, on the heap.
	if !telegram_bot_updates.Ok {
		// something was not ok with the GET request; it is beyond program's control.
		log.Fatal("Get update failed; something external to this program is not working.")
	} else {
		for index, update := range telegram_bot_updates.Result {
			if update.Message.Text == "" && update.Message.Caption == "" { // panic on invalid object (no text AND no caption)
				log.Panicf("Malformed object on index %d with Message ID %d", index, update.Message.MessageId)
			}
			// add each message into the message slice.
			*message_objects_ptr = append(*message_objects_ptr, update.Message)
		}
	}
	return message_objects_ptr // return POINTER to slice of MessageObjects that lives on the heap.
}
