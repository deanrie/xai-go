package xai

// Version is the SDK version sent to the API in the User-Agent and the
// xai-sdk-version metadata, in the same shape the Python SDK uses.
const Version = "0.0.0-dev"

// userAgent mirrors the Python SDK's "XaiSdk/<version>" primary user agent.
const userAgent = "XaiSdkGo/" + Version
