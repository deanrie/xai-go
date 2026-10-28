package xai

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"

	v1 "github.com/deanrie/xai-go/internal/gen/xai/api/v1"
)

// GeneratedImage is one generated image as returned by the API.
type GeneratedImage = v1.GeneratedImage

// ImageResponse is the full image generation response including usage.
type ImageResponse = v1.ImageResponse

// ErrImageModerated is returned by Image.URL and Image.Base64 when the API
// withheld the image for not respecting moderation rules.
var ErrImageModerated = errors.New("xai: image did not respect moderation rules")

// ErrImageFormat is returned when the image was returned in the other
// format than the one asked for.
var ErrImageFormat = errors.New("xai: image not available in the requested format")

// ImageFormat selects how generated images are returned.
type ImageFormat string

// Accepted ImageFormat values.
const (
	ImageFormatURL    ImageFormat = "url"
	ImageFormatBase64 ImageFormat = "base64"
)

var imageFormatValues = map[ImageFormat]v1.ImageFormat{
	ImageFormatURL:    v1.ImageFormat_IMG_FORMAT_URL,
	ImageFormatBase64: v1.ImageFormat_IMG_FORMAT_BASE64,
}

// ImageQuality trades speed for fidelity on grok-imagine models.
type ImageQuality string

// Accepted ImageQuality values.
const (
	ImageQualityLow    ImageQuality = "low"
	ImageQualityMedium ImageQuality = "medium"
	ImageQualityHigh   ImageQuality = "high"
)

var imageQualityValues = map[ImageQuality]v1.ImageQuality{
	ImageQualityLow:    v1.ImageQuality_IMG_QUALITY_LOW,
	ImageQualityMedium: v1.ImageQuality_IMG_QUALITY_MEDIUM,
	ImageQualityHigh:   v1.ImageQuality_IMG_QUALITY_HIGH,
}

// ImageAspectRatio is the output shape, written as in the API docs
// ("1:1", "16:9", "auto", ...).
type ImageAspectRatio string

var imageAspectRatioValues = map[ImageAspectRatio]v1.ImageAspectRatio{
	"1:1":    v1.ImageAspectRatio_IMG_ASPECT_RATIO_1_1,
	"3:4":    v1.ImageAspectRatio_IMG_ASPECT_RATIO_3_4,
	"4:3":    v1.ImageAspectRatio_IMG_ASPECT_RATIO_4_3,
	"9:16":   v1.ImageAspectRatio_IMG_ASPECT_RATIO_9_16,
	"16:9":   v1.ImageAspectRatio_IMG_ASPECT_RATIO_16_9,
	"2:3":    v1.ImageAspectRatio_IMG_ASPECT_RATIO_2_3,
	"3:2":    v1.ImageAspectRatio_IMG_ASPECT_RATIO_3_2,
	"auto":   v1.ImageAspectRatio_IMG_ASPECT_RATIO_AUTO,
	"9:19.5": v1.ImageAspectRatio_IMG_ASPECT_RATIO_9_19_5,
	"19.5:9": v1.ImageAspectRatio_IMG_ASPECT_RATIO_19_5_9,
	"9:20":   v1.ImageAspectRatio_IMG_ASPECT_RATIO_9_20,
	"20:9":   v1.ImageAspectRatio_IMG_ASPECT_RATIO_20_9,
	"1:2":    v1.ImageAspectRatio_IMG_ASPECT_RATIO_1_2,
	"2:1":    v1.ImageAspectRatio_IMG_ASPECT_RATIO_2_1,
}

// ImageResolution is the output resolution on grok-imagine models.
type ImageResolution string

// Accepted ImageResolution values.
const (
	ImageResolution1K ImageResolution = "1k"
	ImageResolution2K ImageResolution = "2k"
)

var imageResolutionValues = map[ImageResolution]v1.ImageResolution{
	ImageResolution1K: v1.ImageResolution_IMG_RESOLUTION_1K,
	ImageResolution2K: v1.ImageResolution_IMG_RESOLUTION_2K,
}

// ImageOption sets a parameter of an image request.
type ImageOption func(*imageRequest)

type imageRequest struct {
	pb  *v1.GenerateImageRequest
	err error
}

func (r *imageRequest) fail(err error) {
	if r.err == nil {
		r.err = err
	}
}

// WithImageFormat picks URL (default) or base64 output.
func WithImageFormat(f ImageFormat) ImageOption {
	return func(r *imageRequest) {
		v, ok := imageFormatValues[f]
		if !ok {
			r.fail(&ValidationError{Param: "format", Value: string(f), Accepted: []string{"url", "base64"}})
			return
		}
		r.pb.Format = v
	}
}

// WithImageQuality sets the quality on grok-imagine models.
func WithImageQuality(q ImageQuality) ImageOption {
	return func(r *imageRequest) {
		v, ok := imageQualityValues[q]
		if !ok {
			r.fail(&ValidationError{Param: "quality", Value: string(q), Accepted: []string{"low", "medium", "high"}})
			return
		}
		r.pb.Quality = v.Enum()
	}
}

// WithImageAspectRatio sets the aspect ratio ("16:9", "auto", ...).
func WithImageAspectRatio(a ImageAspectRatio) ImageOption {
	return func(r *imageRequest) {
		v, ok := imageAspectRatioValues[a]
		if !ok {
			r.fail(&ValidationError{Param: "aspect_ratio", Value: string(a), Accepted: imageAspectRatioNames()})
			return
		}
		r.pb.AspectRatio = v.Enum()
	}
}

// WithImageResolution sets the output resolution ("1k" or "2k").
func WithImageResolution(res ImageResolution) ImageOption {
	return func(r *imageRequest) {
		v, ok := imageResolutionValues[res]
		if !ok {
			r.fail(&ValidationError{Param: "resolution", Value: string(res), Accepted: []string{"1k", "2k"}})
			return
		}
		r.pb.Resolution = v.Enum()
	}
}

// WithSourceImage edits or varies the given image instead of generating
// from the prompt alone.
func WithSourceImage(img *Content) ImageOption {
	return func(r *imageRequest) {
		if img.GetImageUrl() == nil {
			r.fail(&ValidationError{Param: "image", Value: "text content", Accepted: []string{"an image content part from ImageURL"}})
			return
		}
		r.pb.Image = img.GetImageUrl()
	}
}

// WithReferenceImages adds reference images for multi-image editing.
func WithReferenceImages(imgs ...*Content) ImageOption {
	return func(r *imageRequest) {
		for _, img := range imgs {
			if img.GetImageUrl() == nil {
				r.fail(&ValidationError{Param: "images", Value: "text content", Accepted: []string{"image content parts from ImageURL"}})
				return
			}
			r.pb.Images = append(r.pb.Images, img.GetImageUrl())
		}
	}
}

// StorageOptions asks the API to keep a generated image or video in the
// Files API as well as returning it. ExpiresAfter and PublicURLExpiresAfter
// are zero for the server defaults; otherwise one hour to thirty days.
type StorageOptions struct {
	Filename              string
	ExpiresAfter          time.Duration
	PublicURL             bool
	PublicURLExpiresAfter time.Duration
}

func (o StorageOptions) proto() (*v1.StorageOptions, error) {
	if o.Filename == "" {
		return nil, &ValidationError{Param: "storage.filename", Value: "", Accepted: []string{"a non-empty file name"}}
	}
	out := &v1.StorageOptions{Filename: o.Filename}
	if o.ExpiresAfter != 0 {
		if o.ExpiresAfter < time.Hour || o.ExpiresAfter > 30*24*time.Hour {
			return nil, &ValidationError{Param: "storage.expires_after", Value: o.ExpiresAfter, Accepted: []string{"1h to 720h"}}
		}
		out.ExpiresAfter = proto.Int64(int64(o.ExpiresAfter / time.Second))
	}
	if o.PublicURL {
		pu := &v1.PublicUrlOptions{}
		if o.PublicURLExpiresAfter != 0 {
			if o.PublicURLExpiresAfter < time.Hour || o.PublicURLExpiresAfter > 30*24*time.Hour {
				return nil, &ValidationError{Param: "storage.public_url.expires_after", Value: o.PublicURLExpiresAfter, Accepted: []string{"1h to 720h"}}
			}
			pu.ExpiresAfter = proto.Int64(int64(o.PublicURLExpiresAfter / time.Second))
		}
		out.PublicUrl = pu
	}
	return out, nil
}

// WithImageStorage stores the generated image in the Files API; the result
// is on Image.FileOutput.
func WithImageStorage(o StorageOptions) ImageOption {
	return func(r *imageRequest) {
		pb, err := o.proto()
		if err != nil {
			r.fail(err)
			return
		}
		r.pb.StorageOptions = pb
	}
}

// WithImageUser sets the opaque end-user identifier.
func WithImageUser(user string) ImageOption {
	return func(r *imageRequest) { r.pb.User = user }
}

func imageAspectRatioNames() []string {
	names := make([]string, 0, len(imageAspectRatioValues))
	for k := range imageAspectRatioValues {
		names = append(names, string(k))
	}
	return names
}

// Images generates images.
type Images struct{ c *Client }

// Images returns the image generation service.
func (c *Client) Images() *Images { return &Images{c: c} }

// Generate produces one image for the prompt.
func (im *Images) Generate(ctx context.Context, model, prompt string, opts ...ImageOption) (*Image, error) {
	resp, err := im.generate(ctx, model, prompt, 1, opts)
	if err != nil {
		return nil, err
	}
	if len(resp.GetImages()) == 0 {
		return nil, &Error{Code: codes.Internal, Message: "server returned no images"}
	}
	return &Image{pb: resp.GetImages()[0], resp: resp}, nil
}

// GenerateBatch produces n images (1..10) for the prompt.
func (im *Images) GenerateBatch(ctx context.Context, model, prompt string, n int, opts ...ImageOption) ([]*Image, error) {
	if n < 1 || n > 10 {
		return nil, &ValidationError{Param: "n", Value: n, Accepted: []string{"1 to 10"}}
	}
	resp, err := im.generate(ctx, model, prompt, n, opts)
	if err != nil {
		return nil, err
	}
	out := make([]*Image, 0, len(resp.GetImages()))
	for _, g := range resp.GetImages() {
		out = append(out, &Image{pb: g, resp: resp})
	}
	return out, nil
}

func (im *Images) generate(ctx context.Context, model, prompt string, n int, opts []ImageOption) (*ImageResponse, error) {
	r := &imageRequest{pb: &v1.GenerateImageRequest{
		Prompt: prompt,
		Model:  model,
		N:      proto.Int32(int32(n)),
		Format: v1.ImageFormat_IMG_FORMAT_URL,
	}}
	for _, o := range opts {
		o(r)
	}
	if r.err != nil {
		return nil, r.err
	}
	ctx, cancel := im.c.CallContext(ctx)
	defer cancel()
	resp, err := v1.NewImageClient(im.c.conn).GenerateImage(ctx, r.pb)
	if err != nil {
		return nil, wrapErr(err)
	}
	return resp, nil
}

// Image is one generated image with accessors for either output format.
type Image struct {
	pb   *GeneratedImage
	resp *ImageResponse
}

// Proto returns the underlying generated image.
func (i *Image) Proto() *GeneratedImage { return i.pb }

// Response returns the response this image came from (model, usage).
func (i *Image) Response() *ImageResponse { return i.resp }

// FileOutput returns where the image was stored when WithImageStorage was
// used, or nil. StorageError carries the reason when storing failed.
func (i *Image) FileOutput() *v1.FileOutput { return i.pb.GetFileOutput() }

// StorageError returns the server's reason when storing the image failed,
// or an empty string.
func (i *Image) StorageError() string { return i.pb.GetStorageError() }

// URL returns the image URL. ErrImageFormat if the image came back as
// base64 instead, ErrImageModerated if the API withheld it.
func (i *Image) URL() (string, error) {
	return i.pick(i.pb.GetUrl(), i.pb.GetBase64())
}

// Base64 returns the data URL ("data:image/jpeg;base64,...") the API sends
// in base64 mode. ErrImageFormat if the image came back as a URL instead,
// ErrImageModerated if the API withheld it.
func (i *Image) Base64() (string, error) {
	return i.pick(i.pb.GetBase64(), i.pb.GetUrl())
}

// pick returns want, or explains why it is empty: the other format was
// delivered, or nothing was delivered because of moderation.
func (i *Image) pick(want, other string) (string, error) {
	if want != "" {
		return want, nil
	}
	if other != "" || i.pb.GetRespectModeration() {
		return "", ErrImageFormat
	}
	return "", ErrImageModerated
}

// Bytes decodes the base64 image into raw bytes. Only available in base64
// mode; in URL mode fetch the URL yourself.
func (i *Image) Bytes() ([]byte, error) {
	b, err := i.Base64()
	if err != nil {
		return nil, err
	}
	if idx := strings.Index(b, ","); idx >= 0 && strings.HasPrefix(b, "data:") {
		b = b[idx+1:]
	}
	return base64.StdEncoding.DecodeString(b)
}
