package main

import (
	"context"
	"reflect"
	"testing"

	"github.com/original-david-knight/go_wild/tools"
)

func telegramCallbackConfigured(tg *tools.TelegramTools) bool {
	field := reflect.ValueOf(tg).Elem().FieldByName("onNewMessages")
	return field.IsValid() && !field.IsNil()
}

func TestTelegramWorkerStopClearsCallbackImmediately(t *testing.T) {
	tg := tools.NewTelegramTools("test-token")
	tg.SetOnNewMessages(func([]tools.TelegramMessage) {})
	if !telegramCallbackConfigured(tg) {
		t.Fatal("expected callback to be configured before stop")
	}

	stopCtx, cancel := context.WithCancel(context.Background())
	tw := &TelegramWorker{
		agentID: "agent-1",
		tools:   tg,
		cancel:  cancel,
	}

	tw.Stop()

	if telegramCallbackConfigured(tg) {
		t.Fatal("expected Stop to clear callback synchronously")
	}

	select {
	case <-stopCtx.Done():
	default:
		t.Fatal("expected Stop to cancel worker context")
	}
}
