// Command files-chat uploads a document through the Files API and asks a
// question about it, then does the same with the file inlined in the
// request.
//
//	XAI_API_KEY=... go run ./examples/files-chat path/to/document.pdf
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/deanrie/xai-go/xai"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: files-chat <file>")
	}
	path := os.Args[1]
	client, err := xai.New()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()
	ctx := context.Background()

	// Upload once, reference by id as often as needed.
	file, err := client.Files().UploadFile(ctx, path)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = client.Files().Delete(ctx, file.GetId()) }()
	resp, err := client.Chat("grok-4.6").
		Append(xai.User(xai.FileRef(file.GetId()), xai.Text("Summarise this document in three sentences."))).
		Sample(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("via Files API:", resp.Content())

	// Or inline the bytes in the request; nothing is stored.
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	part, err := xai.FileBytes(filepath.Base(path), data, "")
	if err != nil {
		log.Fatal(err)
	}
	resp, err = client.Chat("grok-4.6").
		Append(xai.User(part, xai.Text("What is the title of this document?"))).
		Sample(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("inline:", resp.Content())
}
