package main

import (
	"context"
	"fmt"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

func resolveChannel(ctx context.Context, api *tg.Client, username string) (*tg.Channel, error) {
	resolved, err := api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{
		Username: username,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve @%s: %w", username, err)
	}

	for _, chat := range resolved.Chats {
		if channel, ok := chat.(*tg.Channel); ok {
			return channel, nil
		}
	}

	return nil, fmt.Errorf("@%s is not a channel", username)
}

func joinChannel(ctx context.Context, api *tg.Client, channel *tg.Channel) error {
	if !channel.Left {
		return nil
	}

	_, err := api.ChannelsJoinChannel(ctx, channel.AsInput())
	if err != nil && !tgerr.Is(err, "USER_ALREADY_PARTICIPANT") {
		return fmt.Errorf("join %s: %w", channelName(channel), err)
	}

	channel.Left = false
	return nil
}

func ensureCanPost(ctx context.Context, api *tg.Client, channel *tg.Channel) error {
	if channel.Left {
		return fmt.Errorf("account is not a member of target channel %s", channelName(channel))
	}
	if !channel.Broadcast {
		return nil
	}

	participant, err := api.ChannelsGetParticipant(ctx, &tg.ChannelsGetParticipantRequest{
		Channel:     channel.AsInput(),
		Participant: &tg.InputPeerSelf{},
	})
	if err != nil {
		return fmt.Errorf("check target permissions: %w", err)
	}

	switch p := participant.Participant.(type) {
	case *tg.ChannelParticipantCreator:
		return nil
	case *tg.ChannelParticipantAdmin:
		if p.AdminRights.PostMessages {
			return nil
		}
		return fmt.Errorf("account is admin in %s but lacks the Post messages permission", channelName(channel))
	default:
		return fmt.Errorf("account must be an admin with the Post messages permission in %s", channelName(channel))
	}
}

func channelName(channel *tg.Channel) string {
	if username, ok := channel.GetUsername(); ok && username != "" {
		return "@" + username
	}
	return channel.Title
}
