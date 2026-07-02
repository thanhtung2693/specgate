package command

import "testing"

func TestOpenRouterModelOptions_TextOutputOnly(t *testing.T) {
	t.Parallel()
	models := []openRouterModel{
		{
			ID:   "openai/gpt-text",
			Name: "OpenAI Text",
			Architecture: struct {
				Modality         string   `json:"modality"`
				InputModalities  []string `json:"input_modalities"`
				OutputModalities []string `json:"output_modalities"`
			}{
				OutputModalities: []string{"text"},
			},
		},
		{
			ID:   "google/image-output",
			Name: "Google Image Output",
			Architecture: struct {
				Modality         string   `json:"modality"`
				InputModalities  []string `json:"input_modalities"`
				OutputModalities []string `json:"output_modalities"`
			}{
				OutputModalities: []string{"image", "text"},
			},
		},
		{
			ID:   "vendor/audio-output",
			Name: "Audio Output",
			Architecture: struct {
				Modality         string   `json:"modality"`
				InputModalities  []string `json:"input_modalities"`
				OutputModalities []string `json:"output_modalities"`
			}{
				OutputModalities: []string{"audio"},
			},
		},
	}

	options := openRouterModelOptions(models)
	if len(options) != 2 {
		t.Fatalf("options len = %d, want text model plus manual entry: %#v", len(options), options)
	}
	if options[0].Value != "openai/gpt-text" {
		t.Fatalf("first option = %#v, want text model", options[0])
	}
	if options[1].Value != "" {
		t.Fatalf("last option should be manual entry, got %#v", options[1])
	}
}
