// Command stream prints a streamed answer token by token.
//
//	XAI_API_KEY=... go run ./examples/stream
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/deanrie/xai-go/xai"
)

func main() {
	client, err := xai.New()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	chat := client.Chat("grok-4").Append(xai.User(xai.Text("Tell me a short story about a lighthouse keeper.")))
	stream := chat.Stream(context.Background())
	for _, chunk := range stream.All() {
		fmt.Print(chunk.Content())
	}
	if err := stream.Err(); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\n\n[finish: %s]\n", stream.Response().FinishReason())
}
