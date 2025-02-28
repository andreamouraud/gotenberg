package chromium

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gotenberg/gotenberg/v7/pkg/gotenberg"
	"github.com/gotenberg/gotenberg/v7/pkg/modules/api"
	"github.com/labstack/echo/v4"
	"github.com/microcosm-cc/bluemonday"
	"github.com/russross/blackfriday/v2"
	"go.uber.org/multierr"
)

// FormDataChromiumPDFOptions creates Options form the form data. Fallback to
// default value if the considered key is not present.
func FormDataChromiumPDFOptions(ctx *api.Context) (*api.FormData, Options) {
	defaultOptions := DefaultOptions()

	var (
		failOnConsoleExceptions                          bool
		waitDelay                                        time.Duration
		waitWindowStatus                                 string
		waitForExpression                                string
		userAgent                                        string
		extraHTTPHeaders                                 map[string]string
		emulatedMediaType                                string
		landscape, printBackground                       bool
		scale, paperWidth, paperHeight                   float64
		marginTop, marginBottom, marginLeft, marginRight float64
		pageRanges                                       string
		headerTemplate, footerTemplate                   string
		preferCSSPageSize                                bool
		format                                           string
		width, height, quality                           float64
	)

	form := ctx.FormData().
		Bool("failOnConsoleExceptions", &failOnConsoleExceptions, defaultOptions.FailOnConsoleExceptions).
		Duration("waitDelay", &waitDelay, defaultOptions.WaitDelay).
		String("waitWindowStatus", &waitWindowStatus, defaultOptions.WaitWindowStatus).
		String("waitForExpression", &waitForExpression, defaultOptions.WaitForExpression).
		String("userAgent", &userAgent, defaultOptions.UserAgent).
		Custom("extraHttpHeaders", func(value string) error {
			if value == "" {
				extraHTTPHeaders = defaultOptions.ExtraHTTPHeaders

				return nil
			}

			err := json.Unmarshal([]byte(value), &extraHTTPHeaders)
			if err != nil {
				return fmt.Errorf("unmarshal extra HTTP headers: %w", err)
			}

			return nil
		}).
		Custom("emulatedMediaType", func(value string) error {
			if value == "" {
				emulatedMediaType = defaultOptions.EmulatedMediaType

				return nil
			}

			if value != "screen" && value != "print" {
				return fmt.Errorf("wrong value, expected either 'screen', 'print' or empty")
			}

			emulatedMediaType = value

			return nil
		}).
		Bool("landscape", &landscape, defaultOptions.Landscape).
		Bool("printBackground", &printBackground, defaultOptions.PrintBackground).
		Float64("scale", &scale, defaultOptions.Scale).
		Float64("paperWidth", &paperWidth, defaultOptions.PaperWidth).
		Float64("paperHeight", &paperHeight, defaultOptions.PaperHeight).
		Float64("marginTop", &marginTop, defaultOptions.MarginTop).
		Float64("marginBottom", &marginBottom, defaultOptions.MarginBottom).
		Float64("marginLeft", &marginLeft, defaultOptions.MarginLeft).
		Float64("marginRight", &marginRight, defaultOptions.MarginRight).
		String("nativePageRanges", &pageRanges, defaultOptions.PageRanges).
		Content("header.html", &headerTemplate, defaultOptions.HeaderTemplate).
		Content("footer.html", &footerTemplate, defaultOptions.FooterTemplate).
		Bool("preferCssPageSize", &preferCSSPageSize, defaultOptions.PreferCSSPageSize).
		String("format", &format, defaultOptions.Format).
		Float64("width", &width, defaultOptions.Width).
		Float64("height", &height, defaultOptions.Height).
		Float64("quality", &quality, defaultOptions.Quality)

	options := Options{
		FailOnConsoleExceptions: failOnConsoleExceptions,
		WaitDelay:               waitDelay,
		WaitWindowStatus:        waitWindowStatus,
		WaitForExpression:       waitForExpression,
		UserAgent:               userAgent,
		ExtraHTTPHeaders:        extraHTTPHeaders,
		ExtraLinkTags:           defaultOptions.ExtraLinkTags,
		EmulatedMediaType:       emulatedMediaType,
		ExtraScriptTags:         defaultOptions.ExtraScriptTags,
		Landscape:               landscape,
		PrintBackground:         printBackground,
		Scale:                   scale,
		PaperWidth:              paperWidth,
		PaperHeight:             paperHeight,
		MarginTop:               marginTop,
		MarginBottom:            marginBottom,
		MarginLeft:              marginLeft,
		MarginRight:             marginRight,
		PageRanges:              pageRanges,
		HeaderTemplate:          headerTemplate,
		FooterTemplate:          footerTemplate,
		PreferCSSPageSize:       preferCSSPageSize,
		Format:                  format,
		Width:                   width,
		Height:                  height,
		Quality:                 quality,
	}

	return form, options
}

// convertURLRoute returns an api.Route which can convert a URL to PDF.
func convertURLRoute(chromium API, engine gotenberg.PDFEngine) api.Route {
	return api.Route{
		Method:      http.MethodPost,
		Path:        "/forms/chromium/convert/url",
		IsMultipart: true,
		Handler: func(c echo.Context) error {
			ctx := c.Get("context").(*api.Context)
			form, options := FormDataChromiumPDFOptions(ctx)

			var (
				URL       string
				PDFformat string
			)

			err := form.
				MandatoryString("url", &URL).
				String("pdfFormat", &PDFformat, "").
				Custom("extraLinkTags", func(value string) error {
					if value == "" {
						return nil
					}

					err := json.Unmarshal([]byte(value), &options.ExtraLinkTags)
					if err != nil {
						return fmt.Errorf("unmarshal extra link tags: %w", err)
					}

					return nil
				}).
				Custom("extraScriptTags", func(value string) error {
					if value == "" {
						return nil
					}

					err := json.Unmarshal([]byte(value), &options.ExtraScriptTags)
					if err != nil {
						return fmt.Errorf("unmarshal extra script tags: %w", err)
					}

					return nil
				}).
				Validate()

			if err != nil {
				return fmt.Errorf("validate form data: %w", err)
			}

			err = convertURL(ctx, chromium, engine, URL, PDFformat, options)
			if err != nil {
				return fmt.Errorf("convert URL to PDF: %w", err)
			}

			return nil
		},
	}
}

// convertHTMLRoute returns an api.Route which can convert an HTML file to PDF.
func convertHTMLRoute(chromium API, engine gotenberg.PDFEngine) api.Route {
	return api.Route{
		Method:      http.MethodPost,
		Path:        "/forms/chromium/convert/html",
		IsMultipart: true,
		Handler: func(c echo.Context) error {
			ctx := c.Get("context").(*api.Context)
			form, options := FormDataChromiumPDFOptions(ctx)

			var (
				inputPath string
				PDFformat string
			)

			err := form.
				MandatoryPath("index.html", &inputPath).
				String("pdfFormat", &PDFformat, "").
				Validate()

			if err != nil {
				return fmt.Errorf("validate form data: %w", err)
			}

			URL := fmt.Sprintf("file://%s", inputPath)

			err = convertURL(ctx, chromium, engine, URL, PDFformat, options)
			if err != nil {
				return fmt.Errorf("convert HTML to PDF: %w", err)
			}

			return nil
		},
	}
}

// convertMarkdownRoute returns an api.Route which can convert markdown files
// to PDF.
func convertMarkdownRoute(chromium API, engine gotenberg.PDFEngine) api.Route {
	return api.Route{
		Method:      http.MethodPost,
		Path:        "/forms/chromium/convert/markdown",
		IsMultipart: true,
		Handler: func(c echo.Context) error {
			ctx := c.Get("context").(*api.Context)
			form, options := FormDataChromiumPDFOptions(ctx)

			var (
				inputPath     string
				markdownPaths []string
				PDFformat     string
			)

			err := form.
				MandatoryPath("index.html", &inputPath).
				MandatoryPaths([]string{".md"}, &markdownPaths).
				String("pdfFormat", &PDFformat, "").
				Validate()

			if err != nil {
				return fmt.Errorf("validate form data: %w", err)
			}

			// We have to convert each markdown file referenced in the HTML
			// file to... HTML. Thanks to the "html/template" package, we are
			// able to provide the "toHTML" function which the user may call
			// directly inside the HTML file.

			var markdownFilesNotFoundErr error

			tmpl, err := template.
				New(filepath.Base(inputPath)).
				Funcs(template.FuncMap{
					"toHTML": func(filename string) (template.HTML, error) {
						var path string

						for _, markdownPath := range markdownPaths {
							markdownFilename := filepath.Base(markdownPath)

							if filename == markdownFilename {
								path = markdownPath
								break
							}
						}

						if path == "" {
							markdownFilesNotFoundErr = multierr.Append(
								markdownFilesNotFoundErr,
								fmt.Errorf("'%s'", filename),
							)

							return "", nil
						}

						b, err := os.ReadFile(path)
						if err != nil {
							return "", fmt.Errorf("read markdown file '%s': %w", filename, err)
						}

						unsafe := blackfriday.Run(b)
						sanitized := bluemonday.UGCPolicy().SanitizeBytes(unsafe)

						// #nosec
						return template.HTML(sanitized), nil
					},
				}).ParseFiles(inputPath)

			if err != nil {
				return fmt.Errorf("parse template file: %w", err)
			}

			var buffer bytes.Buffer

			err = tmpl.Execute(&buffer, &struct{}{})
			if err != nil {
				return fmt.Errorf("execute template: %w", err)
			}

			if markdownFilesNotFoundErr != nil {
				return api.WrapError(
					fmt.Errorf("markdown files not found: %w", markdownFilesNotFoundErr),
					api.NewSentinelHTTPError(
						http.StatusBadRequest,
						fmt.Sprintf("Markdown file(s) not found: %s", markdownFilesNotFoundErr),
					),
				)
			}

			inputPath = ctx.GeneratePath(".html")

			err = os.WriteFile(inputPath, buffer.Bytes(), 0600)
			if err != nil {
				return fmt.Errorf("write template result: %w", err)
			}

			URL := fmt.Sprintf("file://%s", inputPath)

			err = convertURL(ctx, chromium, engine, URL, PDFformat, options)
			if err != nil {
				return fmt.Errorf("convert markdown to PDF: %w", err)
			}

			return nil
		},
	}
}

// Find the file extension based on the options
func getOutputExtension(options Options) string {
	if options.Format == "pdf" {
		return ".pdf"
	} else if options.Quality < 100 {
		return ".jpg"
	} else {
		return ".png"
	}
}

// convertURL is a stub which is called by the other methods of this file.
func convertURL(ctx *api.Context, chromium API, engine gotenberg.PDFEngine, URL, PDFformat string, options Options) error {
	outputPath := ctx.GeneratePath(getOutputExtension(options))

	var err error
	if options.Format == "img" {
		err = chromium.Image(ctx, ctx.Log(), URL, outputPath, options)
	} else {
		err = chromium.PDF(ctx, ctx.Log(), URL, outputPath, options)
	}

	if err != nil {

		if errors.Is(err, ErrURLNotAuthorized) {
			return api.WrapError(
				fmt.Errorf("convert to %s: %w", options.Format, err),
				api.NewSentinelHTTPError(
					http.StatusForbidden,
					fmt.Sprintf("'%s' does not match the authorized URLs", URL),
				),
			)
		}

		if errors.Is(err, ErrInvalidEvaluationExpression) {
			if options.WaitForExpression == "" {
				// We do not expect the 'waitWindowStatus' form field to return
				// an ErrInvalidEvaluationExpression error. In such a scenario,
				// we return a 500.
				return fmt.Errorf("convert to %s: %w", options.Format, err)
			}

			return api.WrapError(
				fmt.Errorf("convert to %s: %w", options.Format, err),
				api.NewSentinelHTTPError(
					http.StatusBadRequest,
					fmt.Sprintf("The expression '%s' (waitForExpression) returned an exception or undefined", options.WaitForExpression),
				),
			)
		}

		if errors.Is(err, ErrInvalidPrinterSettings) {
			return api.WrapError(
				fmt.Errorf("convert to %s: %w", options.Format, err),
				api.NewSentinelHTTPError(
					http.StatusBadRequest,
					"Chromium does not handle the provided settings; please check for aberrant form values",
				),
			)
		}

		if errors.Is(err, ErrPageRangesSyntaxError) {
			return api.WrapError(
				fmt.Errorf("convert to %s: %w", options.Format, err),
				api.NewSentinelHTTPError(
					http.StatusBadRequest,
					fmt.Sprintf("Chromium does not handle the page ranges '%s' (nativePageRanges)", options.PageRanges),
				),
			)
		}

		if errors.Is(err, ErrConsoleExceptions) {
			return api.WrapError(
				fmt.Errorf("convert to %s: %w", options.Format, err),
				api.NewSentinelHTTPError(
					http.StatusConflict,
					fmt.Sprintf("Chromium console exceptions:\n %s", strings.ReplaceAll(err.Error(), ErrConsoleExceptions.Error(), "")),
				),
			)
		}

		return fmt.Errorf("convert to %s: %w", options.Format, err)
	}

	// So far so good, the URL has been converted to PDF.
	// Now, let's check if the client want to convert this result PDF
	// to a specific PDF format.

	if PDFformat != "" && options.Format == "pdf" {
		convertInputPath := outputPath
		convertOutputPath := ctx.GeneratePath(".pdf")

		err = engine.Convert(ctx, ctx.Log(), PDFformat, convertInputPath, convertOutputPath)

		if err != nil {
			if errors.Is(err, gotenberg.ErrPDFFormatNotAvailable) {
				return api.WrapError(
					fmt.Errorf("convert PDF: %w", err),
					api.NewSentinelHTTPError(
						http.StatusBadRequest,
						fmt.Sprintf("At least one PDF engine does not handle the PDF format '%s' (pdfFormat), while other have failed to convert for other reasons", PDFformat),
					),
				)
			}

			return fmt.Errorf("convert PDF: %w", err)
		}

		// Important: the output path is now the converted file.
		outputPath = convertOutputPath
	}

	err = ctx.AddOutputPaths(outputPath)
	if err != nil {
		return fmt.Errorf("add output path: %w", err)
	}

	return nil
}
