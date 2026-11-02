package telegram

import (
	"github.com/gotd/td/tg"
	"testing"
)

func TestCollectionLimitsAccountAndBoundary(t *testing.T) {
	config := &tg.JSONObject{Value: []tg.JSONObjectValue{
		{Key: "dialog_filters_limit_default", Value: &tg.JSONNumber{Value: 12}},
		{Key: "dialog_filters_limit_premium", Value: &tg.JSONNumber{Value: 35}},
		{Key: "dialog_filters_chats_limit_premium", Value: &tg.JSONNumber{Value: 250}},
		{Key: "dialog_filters_chats_limit_default", Value: &tg.JSONNumber{Value: 0}},
	}}
	basic := parseCollectionLimits(config, false)
	premium := parseCollectionLimits(config, true)
	if basic.folders != 12 || basic.chats != 100 || premium.folders != 35 || premium.chats != 250 {
		t.Fatalf("wrong limits: %+v %+v", basic, premium)
	}
	if basic.validate(true, 12, 1) == nil {
		t.Fatal("creation allowed past account limit")
	}
	if basic.validate(false, 12, 100) != nil {
		t.Fatal("existing folder at exact limit rejected")
	}
	if basic.validate(false, 1, 101) == nil {
		t.Fatal("oversized membership allowed")
	}
	if parseCollectionLimits(nil, true).chats != 200 {
		t.Fatal("official premium fallback missing")
	}
}

func TestCollectionPeerCountPreservesPinsWithoutDoubleCounting(t *testing.T) {
	filter := &tg.DialogFilter{IncludePeers: []tg.InputPeerClass{&tg.InputPeerChannel{ChannelID: 7}, &tg.InputPeerUser{UserID: 7}}, PinnedPeers: []tg.InputPeerClass{&tg.InputPeerChannel{ChannelID: 7}, &tg.InputPeerChat{ChatID: 7}}}
	if count := collectionPeerCount(filter); count != 3 {
		t.Fatalf("count=%d", count)
	}
}
