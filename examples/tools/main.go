// Command tools runs a function-calling loop: the model asks for the
// weather, the program answers, the model writes the final reply.
//
//	XAI_API_KEY=... go run ./examples/tools
package main

import (
	"context"
	"encoding/json"
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

	weather := xai.FunctionTool("get_weather", "Current weather for a city",
		`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`)
	chat := client.Chat("grok-4.6", xai.WithTools(weather))
	chat.Append(xai.User(xai.Text("What is the weather in London right now?")))

	ctx := context.Background()
	for turn := 0; turn < 5; turn++ {
		resp, err := chat.Sample(ctx)
		if err != nil {
			log.Fatal(err)
		}
		calls := resp.ToolCalls()
		if len(calls) == 0 {
			fmt.Println(resp.Content())
			return
		}
		chat.AppendResponse(resp)
		for _, call := range calls {
			var args struct{ City string }
			_ = json.Unmarshal([]byte(call.GetFunction().GetArguments()), &args)
			fmt.Printf("tool call %s(%q)\n", call.GetFunction().GetName(), args.City)
			chat.Append(xai.ToolResult(call.GetId(), `{"temp_c":12,"sky":"overcast"}`))
		}
	}
	log.Fatal("the model kept calling tools")
}
