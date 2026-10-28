package xai

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/proto"

	v1 "github.com/deanrie/xai-go/internal/gen/xai/api/v1"
)

// VideoResponse is the full video generation response: the video, model,
// usage, progress and any error.
type VideoResponse = v1.VideoResponse

// GeneratedVideo is the generated video itself (URL, duration, storage).
type GeneratedVideo = v1.GeneratedVideo

// VideoError reports a failed video generation with the server's code.
type VideoError struct {
	Code    string
	Message string
}

func (e *VideoError) Error() string {
	return fmt.Sprintf("xai: video generation failed: %s: %s", e.Code, e.Message)
}

// VideoAspectRatio is the output shape ("16:9", "9:16", ...).
type VideoAspectRatio string

var videoAspectRatioValues = map[VideoAspectRatio]v1.VideoAspectRatio{
	"1:1":  v1.VideoAspectRatio_VIDEO_ASPECT_RATIO_1_1,
	"16:9": v1.VideoAspectRatio_VIDEO_ASPECT_RATIO_16_9,
	"9:16": v1.VideoAspectRatio_VIDEO_ASPECT_RATIO_9_16,
	"4:3":  v1.VideoAspectRatio_VIDEO_ASPECT_RATIO_4_3,
	"3:4":  v1.VideoAspectRatio_VIDEO_ASPECT_RATIO_3_4,
	"3:2":  v1.VideoAspectRatio_VIDEO_ASPECT_RATIO_3_2,
	"2:3":  v1.VideoAspectRatio_VIDEO_ASPECT_RATIO_2_3,
}

// VideoResolution is the output resolution.
type VideoResolution string

// Accepted VideoResolution values.
const (
	VideoResolution480p  VideoResolution = "480p"
	VideoResolution720p  VideoResolution = "720p"
	VideoResolution1080p VideoResolution = "1080p"
)

var videoResolutionValues = map[VideoResolution]v1.VideoResolution{
	VideoResolution480p:  v1.VideoResolution_VIDEO_RESOLUTION_480P,
	VideoResolution720p:  v1.VideoResolution_VIDEO_RESOLUTION_720P,
	VideoResolution1080p: v1.VideoResolution_VIDEO_RESOLUTION_1080P,
}

// VideoSource points at an input video by URL or by Files API id.
type VideoSource = v1.VideoUrlContent

// VideoURL returns a video source for a URL.
func VideoURL(url string) *VideoSource {
	return &VideoSource{Source: &v1.VideoUrlContent_Url{Url: url}}
}

// VideoFile returns a video source for an uploaded file.
func VideoFile(fileID string) *VideoSource {
	return &VideoSource{Source: &v1.VideoUrlContent_FileId{FileId: fileID}}
}

// VideoOption sets a parameter of a video generation request.
type VideoOption func(*videoRequest)

type videoRequest struct {
	pb  *v1.GenerateVideoRequest
	err error
}

func (r *videoRequest) fail(err error) {
	if r.err == nil {
		r.err = err
	}
}

// WithVideoDuration sets the length in seconds.
func WithVideoDuration(seconds int32) VideoOption {
	return func(r *videoRequest) { r.pb.Duration = proto.Int32(seconds) }
}

// WithVideoAspectRatio sets the aspect ratio ("16:9", ...).
func WithVideoAspectRatio(a VideoAspectRatio) VideoOption {
	return func(r *videoRequest) {
		v, ok := videoAspectRatioValues[a]
		if !ok {
			r.fail(&ValidationError{Param: "aspect_ratio", Value: string(a), Accepted: []string{"1:1", "16:9", "9:16", "4:3", "3:4", "3:2", "2:3"}})
			return
		}
		r.pb.AspectRatio = v.Enum()
	}
}

// WithVideoResolution sets the resolution ("480p", "720p", "1080p").
func WithVideoResolution(res VideoResolution) VideoOption {
	return func(r *videoRequest) {
		v, ok := videoResolutionValues[res]
		if !ok {
			r.fail(&ValidationError{Param: "resolution", Value: string(res), Accepted: []string{"480p", "720p", "1080p"}})
			return
		}
		r.pb.Resolution = v.Enum()
	}
}

// WithVideoImage animates the given image (image-to-video).
func WithVideoImage(img *Content) VideoOption {
	return func(r *videoRequest) {
		if img.GetImageUrl() == nil {
			r.fail(&ValidationError{Param: "image", Value: "text content", Accepted: []string{"an image content part from ImageURL"}})
			return
		}
		r.pb.Image = img.GetImageUrl()
	}
}

// WithVideoStorage stores the generated video in the Files API; the result
// is on VideoResponse.GetVideo().GetFileOutput().
func WithVideoStorage(o StorageOptions) VideoOption {
	return func(r *videoRequest) {
		pb, err := o.proto()
		if err != nil {
			r.fail(err)
			return
		}
		r.pb.StorageOptions = pb
	}
}

// WithVideoInput edits the given video (video-to-video).
func WithVideoInput(src *VideoSource) VideoOption {
	return func(r *videoRequest) { r.pb.Video = src }
}

// WithVideoReferenceImages adds style or content references
// (reference-to-video).
func WithVideoReferenceImages(imgs ...*Content) VideoOption {
	return func(r *videoRequest) {
		for _, img := range imgs {
			if img.GetImageUrl() == nil {
				r.fail(&ValidationError{Param: "reference_images", Value: "text content", Accepted: []string{"image content parts from ImageURL"}})
				return
			}
			r.pb.ReferenceImages = append(r.pb.ReferenceImages, img.GetImageUrl())
		}
	}
}

// Videos generates videos. Generation is asynchronous on the server; the
// client polls for the result.
type Videos struct{ c *Client }

// Videos returns the video generation service.
func (c *Client) Videos() *Videos { return &Videos{c: c} }

// Start submits a generation request and returns its id for Get or Wait.
func (vs *Videos) Start(ctx context.Context, model, prompt string, opts ...VideoOption) (string, error) {
	r := &videoRequest{pb: &v1.GenerateVideoRequest{Prompt: prompt, Model: model}}
	for _, o := range opts {
		o(r)
	}
	if r.err != nil {
		return "", r.err
	}
	ctx, cancel := vs.c.CallContext(ctx)
	defer cancel()
	resp, err := v1.NewVideoClient(vs.c.conn).GenerateVideo(ctx, r.pb)
	if err != nil {
		return "", wrapErr(err)
	}
	return resp.GetRequestId(), nil
}

// StartExtend submits a request to extend an existing video.
func (vs *Videos) StartExtend(ctx context.Context, model, prompt string, video *VideoSource, duration int32) (string, error) {
	req := &v1.ExtendVideoRequest{Prompt: prompt, Model: model, Video: video}
	if duration > 0 {
		req.Duration = proto.Int32(duration)
	}
	ctx, cancel := vs.c.CallContext(ctx)
	defer cancel()
	resp, err := v1.NewVideoClient(vs.c.conn).ExtendVideo(ctx, req)
	if err != nil {
		return "", wrapErr(err)
	}
	return resp.GetRequestId(), nil
}

// Status is one poll of a running generation.
type VideoStatus struct {
	Done bool
	// Progress is the server's 0..100 estimate while running.
	Progress int32
	// Response is set once Done.
	Response *VideoResponse
}

// Get polls a request once.
func (vs *Videos) Get(ctx context.Context, requestID string) (*VideoStatus, error) {
	ctx, cancel := vs.c.CallContext(ctx)
	defer cancel()
	r, err := v1.NewVideoClient(vs.c.conn).GetDeferredVideo(ctx, &v1.GetDeferredVideoRequest{RequestId: requestID})
	if err != nil {
		return nil, wrapErr(err)
	}
	switch r.GetStatus() {
	case v1.DeferredStatus_DONE:
		resp := r.GetResponse()
		if resp == nil {
			return nil, fmt.Errorf("xai: video request %q: done without a response", requestID)
		}
		return &VideoStatus{Done: true, Progress: 100, Response: resp}, nil
	case v1.DeferredStatus_PENDING:
		return &VideoStatus{Progress: r.GetResponse().GetProgress()}, nil
	case v1.DeferredStatus_EXPIRED:
		return nil, ErrDeferredExpired
	case v1.DeferredStatus_FAILED:
		if e := r.GetResponse().GetError(); e != nil {
			return nil, &VideoError{Code: e.GetCode(), Message: e.GetMessage()}
		}
		return nil, &VideoError{Code: "UNKNOWN", Message: "no error details"}
	case v1.DeferredStatus_INVALID_DEFERRED_STATUS:
		return nil, fmt.Errorf("xai: video request %q: status not set", requestID)
	default:
		return nil, fmt.Errorf("xai: video request %q: unknown status %v", requestID, r.GetStatus())
	}
}

// Wait polls until the request is done. onProgress, if set, receives each
// progress value. Defaults follow DefaultPollInterval and
// DefaultPollTimeout; video generation commonly needs a few minutes.
func (vs *Videos) Wait(ctx context.Context, requestID string, opts PollOptions, onProgress func(int32)) (*VideoResponse, error) {
	var result *VideoResponse
	err := poll(ctx, opts, func(ctx context.Context) (bool, error) {
		st, err := vs.Get(ctx, requestID)
		if err != nil {
			return false, err
		}
		if onProgress != nil {
			onProgress(st.Progress)
		}
		if st.Done {
			result = st.Response
		}
		return st.Done, nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Generate is Start followed by Wait.
func (vs *Videos) Generate(ctx context.Context, model, prompt string, poll PollOptions, opts ...VideoOption) (*VideoResponse, error) {
	id, err := vs.Start(ctx, model, prompt, opts...)
	if err != nil {
		return nil, err
	}
	return vs.Wait(ctx, id, poll, nil)
}

// Extend is StartExtend followed by Wait.
func (vs *Videos) Extend(ctx context.Context, model, prompt string, video *VideoSource, duration int32, poll PollOptions) (*VideoResponse, error) {
	id, err := vs.StartExtend(ctx, model, prompt, video, duration)
	if err != nil {
		return nil, err
	}
	return vs.Wait(ctx, id, poll, nil)
}
