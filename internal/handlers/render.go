package handlers

import (
	"github.com/a-h/templ"
	"github.com/labstack/echo/v4"
)

// render renders a templ component to the response.
func render(c echo.Context, status int, component templ.Component) error {
	c.Response().Header().Set("Content-Type", "text/html; charset=utf-8")
	c.Response().WriteHeader(status)
	return component.Render(c.Request().Context(), c.Response())
}
