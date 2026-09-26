package migrate

import (
	"sort"

	"github.com/ossmalaysia/claude-sync/internal/store"
)

// transcriptsToSend returns the chat transcripts of each project, oldest chat
// first, when the user chose to copy chats; otherwise nil.
func transcriptsToSend(st *store.Store) (map[string][]store.ChatRecord, error) {
	settings, err := st.LoadSettings()
	if err != nil || settings.ChatChoice != store.ChatsInclude {
		return nil, err
	}
	chats, err := st.ListChats()
	if err != nil {
		return nil, err
	}
	sort.Slice(chats, func(i, j int) bool {
		if chats[i].CreatedAt != chats[j].CreatedAt {
			return chats[i].CreatedAt < chats[j].CreatedAt
		}
		return chats[i].UUID < chats[j].UUID
	})
	out := map[string][]store.ChatRecord{}
	for _, c := range chats {
		if c.ProjectUUID != "" && c.Transcript != "" {
			out[c.ProjectUUID] = append(out[c.ProjectUUID], c)
		}
	}
	return out, nil
}
