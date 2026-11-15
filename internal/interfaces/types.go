package interfaces

import sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"

type TranslateRequestFunc = sdktranslator.RequestTransform

type TranslateResponseFunc = sdktranslator.ResponseStreamTransform

type TranslateResponseNonStreamFunc = sdktranslator.ResponseNonStreamTransform

type TranslateResponse = sdktranslator.ResponseTransform
