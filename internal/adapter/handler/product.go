package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// ProductHandler handles product catalog endpoints.
type ProductHandler struct {
	products *app.ProductService
}

func NewProductHandler(products *app.ProductService) *ProductHandler {
	return &ProductHandler{products: products}
}

// ListProducts returns all active products.
// GET /api/v1/products
func (h *ProductHandler) ListProducts(c *gin.Context) {
	list, err := h.products.ListActive(c.Request.Context())
	if err != nil {
		respondInternalError(c, "product.list_active", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"products": list})
}

// ListPlans returns available plans for a product.
// GET /api/v1/products/:id/plans
func (h *ProductHandler) ListPlans(c *gin.Context) {
	productID := c.Param("id")
	plans, err := h.products.ListPlans(c.Request.Context(), productID)
	if err != nil {
		respondInternalError(c, "product.list_plans", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"plans": plans})
}

// AdminCreateProduct creates a new product.
// POST /admin/v1/products
func (h *ProductHandler) AdminCreateProduct(c *gin.Context) {
	var p entity.Product
	if err := c.ShouldBindJSON(&p); err != nil {
		handleBindError(c, err)
		return
	}
	if err := h.products.CreateProduct(c.Request.Context(), &p); err != nil {
		respondInternalError(c, "handler", err)
		return
	}
	c.JSON(http.StatusCreated, p)
}

// AdminUpdateProduct updates a product.
// PUT /admin/v1/products/:id
func (h *ProductHandler) AdminUpdateProduct(c *gin.Context) {
	id := c.Param("id")
	p, err := h.products.GetByID(c.Request.Context(), id)
	if err != nil || p == nil {
		respondNotFound(c, "Product")
		return
	}
	if err := c.ShouldBindJSON(p); err != nil {
		handleBindError(c, err)
		return
	}
	if err := h.products.UpdateProduct(c.Request.Context(), p); err != nil {
		respondInternalError(c, "handler", err)
		return
	}
	c.JSON(http.StatusOK, p)
}

// AdminCreatePlan creates a plan for a product.
// POST /admin/v1/products/:id/plans
func (h *ProductHandler) AdminCreatePlan(c *gin.Context) {
	productID := c.Param("id")
	var plan entity.ProductPlan
	if err := c.ShouldBindJSON(&plan); err != nil {
		handleBindError(c, err)
		return
	}
	plan.ProductID = productID
	if err := h.products.CreatePlan(c.Request.Context(), &plan); err != nil {
		respondInternalError(c, "handler", err)
		return
	}
	c.JSON(http.StatusCreated, plan)
}

// AdminUpdatePlan updates an existing plan.
// PUT /admin/v1/plans/:id
func (h *ProductHandler) AdminUpdatePlan(c *gin.Context) {
	id, ok := parsePathInt64(c, "id", "Plan ID")
	if !ok {
		return
	}
	plan, err := h.products.GetPlanByID(c.Request.Context(), id)
	if err != nil || plan == nil {
		respondNotFound(c, "Plan")
		return
	}
	if err := c.ShouldBindJSON(plan); err != nil {
		handleBindError(c, err)
		return
	}
	if err := h.products.UpdatePlan(c.Request.Context(), plan); err != nil {
		respondInternalError(c, "handler", err)
		return
	}
	c.JSON(http.StatusOK, plan)
}
