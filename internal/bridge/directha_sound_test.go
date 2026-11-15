package bridge

import (
	"context"
	"github.com/coder/websocket"
	fileplayer "github.com/skrashevich/quasar-go-genproto/proto/alice/protos/endpoint/capabilities/audio_file_player"
	"google.golang.org/protobuf/proto"
	"quasarbridge/internal/sound"
	"testing"
)

func TestDirectHASuccessSoundHasNoSpeech(t *testing.T) {
	n, up, down := soundSession(t, resolverFunc(func(ctx context.Context, _ string) (Resolution, error) {
		err := sound.Play(ctx, sound.Request{Sound: "confirmation", Title: "direct-ha-confirmation"})
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
			var playback fileplayer.TAudioFilePlayerCapability_TLocalAudioFilePlayDirective
			if err := proto.Unmarshal(directives[0].GetPayloadRaw(), &playback); err != nil {
				t.Fatal(err)
			}
			if playback.GetSoundToPlay() != fileplayer.TAudioFilePlayerCapability_VolumeSound {
				t.Fatal("Mini 1 confirmation must use VolumeSound")
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
