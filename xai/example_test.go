package xai_test

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/deanrie/xai-go/xai"
)

func ExampleNew() {
	client, err := xai.New(xai.WithAPIKey("xai-..."))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()
}

func ExampleChat_Sample() {
	client, err := xai.New()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	chat := client.Chat("grok-4", xai.WithMaxTokens(100))
	chat.Append(xai.System("Answer in one sentence."))
	chat.Append(xai.User(xai.Text("Why is the sky blue?")))

	resp, err := chat.Sample(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(resp.Content())
}

func ExampleChat_Stream() {
	client, err := xai.New()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	chat := client.Chat("grok-4").Append(xai.User(xai.Text("Tell me a story.")))
	stream := chat.Stream(context.Background())
	for _, chunk := range stream.All() {
		fmt.Print(chunk.Content())
	}
	if err := stream.Err(); err != nil {
		log.Fatal(err)
	}
}

func ExampleChat_Sample_toolCalls() {
	client, err := xai.New()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	weather := xai.FunctionTool("get_weather", "Current weather for a city",
		`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`)
	chat := client.Chat("grok-4", xai.WithTools(weather))
	chat.Append(xai.User(xai.Text("What is the weather in London?")))

	ctx := context.Background()
	resp, err := chat.Sample(ctx)
	if err != nil {
		log.Fatal(err)
	}
	for _, call := range resp.ToolCalls() {
		// Run the function with call.GetFunction().GetArguments(), then:
		chat.AppendResponse(resp).Append(xai.ToolResult(call.GetId(), `{"temp_c":12}`))
	}
	final, err := chat.Sample(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(final.Content())
}

func ExampleError() {
	client, err := xai.New()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	_, err = client.Models().GetLanguageModel(context.Background(), "no-such-model")
	if errors.Is(err, xai.ErrNotFound) {
		fmt.Println("unknown model")
	}
	var apiErr *xai.Error
	if errors.As(err, &apiErr) {
		fmt.Println(apiErr.Code, apiErr.Message)
	}
}
