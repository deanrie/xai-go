// Command structured asks for a JSON answer that matches a schema and
// decodes it into a Go struct.
//
//	XAI_API_KEY=... go run ./examples/structured
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/deanrie/xai-go/xai"
)

type weather struct {
	City        string  `json:"city"`
	Temperature float64 `json:"temperature_c"`
	Sky         string  `json:"sky"`
}

const schema = `{"type":"object","properties":{"city":{"type":"string"},"temperature_c":{"type":"number"},"sky":{"type":"string"}},"required":["city","temperature_c","sky"]}`

func main() {
	client, err := xai.New()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	var out weather
	resp, err := client.Chat("grok-4.6").
		Append(xai.User(xai.Text("Describe typical October weather in Lisbon as JSON."))).
		SampleJSON(context.Background(), schema, &out)
	if err != nil {
		log.Fatalf("%v (raw: %s)", err, resp.Content())
	}
	fmt.Printf("%s: %.0f C, %s\n", out.City, out.Temperature, out.Sky)
}
