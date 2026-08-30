package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

func NewRouter(repository *Repository, simulator *Simulator, uiPath string, hardwareDelaySeconds ...int) *gin.Engine {
	return newRouter(repository, simulator, uiPath, dashboardDelaySeconds(hardwareDelaySeconds))
}

func NewRouterWithMockSAP(repository *Repository, simulator *Simulator, uiPath string, hardwareDelaySeconds int, mockSAPURL, mockSAPToken string) *gin.Engine {
	return newRouter(repository, simulator, uiPath, hardwareDelaySeconds, mockSAPURL, mockSAPToken)
}

func newRouter(repository *Repository, simulator *Simulator, uiPath string, delaySeconds int, mockSAPConfig ...string) *gin.Engine {
	mockSAPURL, mockSAPToken := "", ""
	if len(mockSAPConfig) >= 2 {
		mockSAPURL, mockSAPToken = mockSAPConfig[0], mockSAPConfig[1]
	}
	router := gin.New()
	router.Use(gin.Recovery())
	router.GET("/health", func(c *gin.Context) {
		if err := repository.Ping(c.Request.Context()); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	api := router.Group("/api/dashboard")
	api.GET("/summary", summaryHandler(repository, delaySeconds))
	api.GET("/orders", listOrdersHandler(repository))
	api.GET("/orders/:orderId", orderDetailHandler(repository, delaySeconds))
	api.GET("/orders/:orderId/workflow", workflowHandler(repository, delaySeconds))
	api.POST("/simulations/order", orderSimulationHandler(simulator))
	api.POST("/simulations/payment", paymentSimulationHandler(simulator))
	api.GET("/mock-sap/response", mockSAPResponseHandler(mockSAPURL, mockSAPToken))
	api.PUT("/mock-sap/response", mockSAPResponseHandler(mockSAPURL, mockSAPToken))
	api.DELETE("/mock-sap/response", mockSAPResponseHandler(mockSAPURL, mockSAPToken))

	indexPath := filepath.Join(uiPath, "index.html")
	router.GET("/", func(c *gin.Context) { c.File(indexPath) })
	router.Static("/assets", filepath.Join(uiPath, "assets"))
	router.NoRoute(func(c *gin.Context) {
		if c.Request.Method == http.MethodGet && !strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.File(indexPath)
			return
		}
		c.JSON(http.StatusNotFound, gin.H{"error": "Not found"})
	})
	return router
}

func mockSAPResponseHandler(baseURL, token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if baseURL == "" {
			c.JSON(http.StatusBadGateway, gin.H{"error": "Mock SAP URL is not configured"})
			return
		}
		target, err := url.JoinPath(strings.TrimRight(baseURL, "/"), "/api/admin/response")
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "Invalid Mock SAP URL"})
			return
		}
		var body io.Reader
		if c.Request.Body != nil {
			data, readErr := io.ReadAll(c.Request.Body)
			if readErr != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Unable to read request"})
				return
			}
			body = bytes.NewReader(data)
		}
		req, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, target, body)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "Unable to create Mock SAP request"})
			return
		}
		req.Header.Set("X-Mock-SAP-Admin-Token", token)
		req.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "Unable to reach Mock SAP", "detail": err.Error()})
			return
		}
		defer response.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		c.Data(response.StatusCode, response.Header.Get("Content-Type"), data)
	}
}

func summaryHandler(repository *Repository, hardwareDelaySeconds ...int) gin.HandlerFunc {
	return func(c *gin.Context) {
		summary, err := repository.Summary(c.Request.Context())
		if err != nil {
			internalError(c, err)
			return
		}
		summary.HardwareDelaySeconds = dashboardDelaySeconds(hardwareDelaySeconds)
		c.JSON(http.StatusOK, summary)
	}
}

func listOrdersHandler(repository *Repository) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
		orders, err := repository.ListOrders(c.Request.Context(), c.Query("q"), c.Query("status"), limit)
		if err != nil {
			internalError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"orders": orders})
	}
}

func orderDetailHandler(repository *Repository, hardwareDelaySeconds ...int) gin.HandlerFunc {
	return func(c *gin.Context) {
		detail, err := repository.GetOrder(c.Request.Context(), c.Param("orderId"))
		if errors.Is(err, ErrOrderNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Order not found"})
			return
		}
		if err != nil {
			internalError(c, err)
			return
		}
		detailResponse := gin.H{"order": detail.Order, "items": detail.Items, "events": detail.Events, "workflow": BuildWorkflow(detail, dashboardDelaySeconds(hardwareDelaySeconds))}
		c.JSON(http.StatusOK, detailResponse)
	}
}

func workflowHandler(repository *Repository, hardwareDelaySeconds ...int) gin.HandlerFunc {
	return func(c *gin.Context) {
		detail, err := repository.GetOrder(c.Request.Context(), c.Param("orderId"))
		if errors.Is(err, ErrOrderNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Order not found"})
			return
		}
		if err != nil {
			internalError(c, err)
			return
		}
		c.JSON(http.StatusOK, BuildWorkflow(detail, dashboardDelaySeconds(hardwareDelaySeconds)))
	}
}

func dashboardDelaySeconds(value []int) int {
	if len(value) > 0 {
		return value[0]
	}
	return 30
}

func orderSimulationHandler(simulator *Simulator) gin.HandlerFunc {
	return func(c *gin.Context) {
		var request OrderSimulationRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid simulation request", "detail": err.Error()})
			return
		}
		result, err := simulator.RunOrder(c.Request.Context(), request)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "Simulation failed", "detail": err.Error()})
			return
		}
		c.JSON(http.StatusAccepted, result)
	}
}

func paymentSimulationHandler(simulator *Simulator) gin.HandlerFunc {
	return func(c *gin.Context) {
		var request PaymentSimulationRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid simulation request", "detail": err.Error()})
			return
		}
		result, err := simulator.RunPayment(c.Request.Context(), request)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "Simulation failed", "detail": err.Error()})
			return
		}
		c.JSON(http.StatusAccepted, result)
	}
}

func internalError(c *gin.Context, err error) {
	_ = err
	c.JSON(http.StatusInternalServerError, gin.H{"error": "Dashboard query failed"})
}
