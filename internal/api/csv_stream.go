package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

// streamCSV writes a CSV attachment by running write in a background goroutine connected via io.Pipe.
func streamCSV(c echo.Context, filename string, write func(ctx context.Context, w io.Writer) error) error {
	c.Response().Header().Set(echo.HeaderContentType, "text/csv")
	c.Response().Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.csv", filename))
	pipeReader, pipeWriter := io.Pipe()
	reqCtx := c.Request().Context()
	go func() {
		var genErr error
		defer func() {
			if r := recover(); r != nil {
				genErr = fmt.Errorf("panic in CSV generation: %v", r)
			}
			if genErr != nil {
				_ = pipeWriter.CloseWithError(genErr)
			} else {
				_ = pipeWriter.Close()
			}
		}()
		genErr = write(reqCtx, pipeWriter)
	}()
	return c.Stream(http.StatusOK, "text/csv", pipeReader)
}

func csvFilename(prefix string) string {
	return prefix + "-" + time.Now().Format("20060102")
}
