// Command chat sends one question to grok-4 and prints the answer.
//
//	XAI_API_KEY=... go run ./examples/chat "Why is the sky blue?"
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
	question := "Why is the sky blue?"
	if len(os.Args) > 1 {
		question = strings.Join(os.Args[1:], " ")
	}
	client, err := xai.New()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	chat := client.Chat("grok-4", xai.WithMaxTokens(300))
	chat.Append(xai.System("Answer in two sentences.")).Append(xai.User(xai.Text(question)))

	resp, err := chat.Sample(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(resp.Content())
	fmt.Printf("\n[%s, %d tokens]\n", resp.Proto().GetModel(), resp.Usage().GetTotalTokens())
}
