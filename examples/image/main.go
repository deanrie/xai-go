// Command image generates one image and writes it to disk, once through
// the image API and once through the chat image_generation tool.
//
//	XAI_API_KEY=... go run ./examples/image "a lighthouse at dusk, flat illustration"
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/deanrie/xai-go/xai"
)

func main() {
	prompt := "a lighthouse at dusk, flat illustration"
	if len(os.Args) > 1 {
		prompt = strings.Join(os.Args[1:], " ")
	}
	client, err := xai.New()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()
	ctx := context.Background()

	img, err := client.Images().Generate(ctx, "grok-imagine-image", prompt, xai.WithImageFormat(xai.ImageFormatBase64))
	if err != nil {
		log.Fatal(err)
	}
	raw, err := img.Bytes()
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("image-api.jpg", raw, 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("image API: wrote image-api.jpg (%d bytes)\n", len(raw))

	tool, err := xai.ImageGenerationTool(xai.ImageGenerationGenerate)
	if err != nil {
		log.Fatal(err)
	}
	resp, err := client.Chat("grok-4.6", xai.WithTools(tool)).
		Append(xai.User(xai.Text("Generate an image: " + prompt))).
		Sample(ctx)
	if err != nil {
		log.Fatal(err)
	}
	outs, err := resp.ImageOutputs()
	if err != nil {
		log.Fatal(err)
	}
	for i, o := range outs {
		name := fmt.Sprintf("image-tool-%d.%s", i, strings.TrimPrefix(o.MIMEType, "image/"))
		if err := os.WriteFile(name, o.Image, 0o644); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("chat tool: wrote %s (%d bytes, uuid %s)\n", name, len(o.Image), o.ImageUUID)
	}
	fmt.Println(resp.Content())
}
