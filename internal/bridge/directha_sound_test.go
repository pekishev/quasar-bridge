package bridge

import (
	"context"
	"github.com/coder/websocket"
	"quasarbridge/internal/sound"
	"testing"
)

func TestDirectHASuccessSoundHasNoSpeech(t *testing.T) {
	n, up, down := soundSession(t, resolverFunc(func(ctx context.Context, _ string) (Resolution, error) {
		err := sound.Play(ctx, sound.Request{Sound: "success", Title: "direct-ha-confirmation"})
		return Resolution{Route: "home"}, err
	}))
	n.ShouldListen = true
	if err := n.Client(t.Context(), websocket.MessageBinary, soundInput("direct-ha-success")); err != nil {
		t.Fatal(err)
	}
	for {
		frame := nextSoundFrame(t, down)
		if response := frame.Server.GetEvent().GetAliceResponse(); response != nil {
			if response.GetVoiceResponse().GetHasVoiceResponse() || response.GetVoiceResponse().GetOutputSpeech().GetText() != "" || response.GetVoiceResponse().GetShouldListen() {
				t.Fatal("confirmation must be sound-only without reopening listening")
			}
			directives := response.GetResponse().GetDirectives()
			if len(directives) != 1 || directives[0].GetName() != "local_audio_file_play" {
				t.Fatal("missing built-in confirmation sound")
			}
			break
		}
	}
	n.Close()
	for len(up) > 0 {
		if frame := <-up; frame.Kind() == "TtsGenerate" {
			t.Fatal("sound confirmation requested cloud speech")
		}
	}
}
