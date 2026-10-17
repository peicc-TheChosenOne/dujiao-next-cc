package userhttp

import (
	"net/http"
	"strconv"
	"strings"

	resellermodule "github.com/dujiao-next/internal/modules/reseller/application"
	dto "github.com/dujiao-next/internal/modules/reseller/transport/http/presenter"
	transportshared "github.com/dujiao-next/internal/modules/reseller/transport/http/shared"
	"github.com/dujiao-next/internal/platform/http/ginutil"
	"github.com/dujiao-next/internal/platform/http/response"

	"github.com/gin-gonic/gin"
)

type UserProductImagesHandler struct {
	service *resellermodule.ProductImageService
}

func NewUserProductImagesHandler(service *resellermodule.ProductImageService) *UserProductImagesHandler {
	if service == nil {
		panic("reseller product images handler: service is nil")
	}
	return &UserProductImagesHandler{service: service}
}

func (h *UserProductImagesHandler) List(c *gin.Context) {
	uid, ok := ginutil.GetUserID(c)
	if !ok {
		return
	}
	page, size := ginutil.ParsePagination(c)
	rows, total, err := h.service.ListUserProductImages(uid, resellermodule.ProductSettingUserListInput{Page: page, PageSize: size, Keyword: strings.TrimSpace(c.Query("keyword"))})
	if err != nil {
		respondUserProductImagesError(c, err)
		return
	}
	result := make([]dto.ResellerProductImagesResp, 0, len(rows))
	for _, row := range rows {
		result = append(result, dto.NewResellerProductImagesResp(row.Product, row.Config))
	}
	response.SuccessWithPage(c, result, response.BuildPagination(page, size, total))
}

func (h *UserProductImagesHandler) Get(c *gin.Context) {
	uid, ok := ginutil.GetUserID(c)
	if !ok {
		return
	}
	id, err := ginutil.ParseParamUint(c, "product_id")
	if err != nil {
		ginutil.RespondBindError(c, err)
		return
	}
	state, err := h.service.GetUserProductImages(uid, id)
	if err != nil {
		respondUserProductImagesError(c, err)
		return
	}
	response.Success(c, dto.NewResellerProductImagesResp(state.Product, state.Config))
}

func (h *UserProductImagesHandler) Upload(c *gin.Context) {
	h.upload(c, false)
}

func (h *UserProductImagesHandler) UploadDetail(c *gin.Context) {
	h.upload(c, true)
}

func (h *UserProductImagesHandler) upload(c *gin.Context, detail bool) {
	uid, ok := ginutil.GetUserID(c)
	if !ok {
		return
	}
	id, err := ginutil.ParseParamUint(c, "product_id")
	if err != nil {
		ginutil.RespondBindError(c, err)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, resellermodule.ProductImageMaxSize+64*1024)
	file, err := c.FormFile("file")
	if err != nil {
		ginutil.RespondError(c, response.CodeBadRequest, "error.reseller_product_images_invalid", nil)
		return
	}
	if c.Request.MultipartForm != nil {
		defer c.Request.MultipartForm.RemoveAll()
		if detail && len(c.Request.MultipartForm.File["file"]) != 1 {
			ginutil.RespondError(c, response.CodeBadRequest, "error.reseller_product_images_invalid", nil)
			return
		}
	}
	revision, err := strconv.ParseUint(c.PostForm("revision"), 10, 64)
	if err != nil {
		ginutil.RespondBindError(c, err)
		return
	}
	var state *resellermodule.ProductImageState
	if detail {
		state, err = h.service.UploadUserProductDetailImage(uid, id, file, revision, c.PostForm("operation_id"))
	} else {
		state, err = h.service.UploadUserProductImage(uid, id, file, revision, c.PostForm("operation_id"))
	}
	if err != nil {
		respondUserProductImagesError(c, err)
		return
	}
	response.Success(c, dto.NewResellerProductImagesResp(state.Product, state.Config))
}

func (h *UserProductImagesHandler) ResetDetail(c *gin.Context) {
	uid, ok := ginutil.GetUserID(c)
	if !ok {
		return
	}
	id, err := ginutil.ParseParamUint(c, "product_id")
	if err != nil {
		ginutil.RespondBindError(c, err)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16*1024)
	var req struct {
		Revision    *uint64 `json:"revision" binding:"required"`
		OperationID string  `json:"operation_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		ginutil.RespondBindError(c, err)
		return
	}
	state, err := h.service.ResetUserProductDetailImage(uid, id, *req.Revision, req.OperationID)
	if err != nil {
		respondUserProductImagesError(c, err)
		return
	}
	response.Success(c, dto.NewResellerProductImagesResp(state.Product, state.Config))
}

func (h *UserProductImagesHandler) Update(c *gin.Context) {
	uid, ok := ginutil.GetUserID(c)
	if !ok {
		return
	}
	id, err := ginutil.ParseParamUint(c, "product_id")
	if err != nil {
		ginutil.RespondBindError(c, err)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16*1024)
	var req struct {
		Images      *[]string `json:"images" binding:"required"`
		Revision    *uint64   `json:"revision" binding:"required"`
		OperationID string    `json:"operation_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		ginutil.RespondBindError(c, err)
		return
	}
	state, err := h.service.UpdateUserProductImages(uid, id, *req.Images, *req.Revision, req.OperationID)
	if err != nil {
		respondUserProductImagesError(c, err)
		return
	}
	response.Success(c, dto.NewResellerProductImagesResp(state.Product, state.Config))
}

// UserProductSettingService 是用户中心分销商品配置端点所需的最小用例接口。
type UserProductSettingService interface {
	ListUserProductSettings(userID uint, input resellermodule.ProductSettingUserListInput) ([]resellermodule.ProductSettingListRow, int64, error)
	GetUserProductSetting(userID, productID uint) (*resellermodule.ProductSettingDetail, error)
	PreviewUserProductSettings(userID, productID uint, input resellermodule.ProductSettingSaveInput) ([]resellermodule.ProductSettingPreviewItem, error)
	SaveUserProductSettings(userID, productID uint, input resellermodule.ProductSettingSaveInput) (*resellermodule.ProductSettingDetail, error)
	ResetUserProductSetting(userID, productID, skuID uint) error
}

// UserProductSettingHandler 处理用户中心分销商品配置请求。
type UserProductSettingHandler struct {
	service UserProductSettingService
}

func NewUserProductSettingHandler(service UserProductSettingService) *UserProductSettingHandler {
	if service == nil {
		panic("reseller user product setting handler: service is nil")
	}
	return &UserProductSettingHandler{service: service}
}

// ListProductSettings 查询当前用户可配置的分销商品。
func (h *UserProductSettingHandler) ListProductSettings(c *gin.Context) {
	uid, ok := ginutil.GetUserID(c)
	if !ok {
		return
	}
	page, pageSize := ginutil.ParsePagination(c)
	categoryID, _ := ginutil.ParseQueryUint(c.Query("category_id"), false)
	rows, total, err := h.service.ListUserProductSettings(uid, resellermodule.ProductSettingUserListInput{
		Page:       page,
		PageSize:   pageSize,
		Keyword:    strings.TrimSpace(c.Query("keyword")),
		CategoryID: categoryID,
		Configured: strings.TrimSpace(c.Query("configured")),
		Listed:     strings.TrimSpace(c.Query("listed")),
	})
	if err != nil {
		respondUserProductSettingError(c, err, "error.user_fetch_failed")
		return
	}
	response.SuccessWithPage(c, dto.NewResellerProductSettingListResp(transportshared.ListDTOInput(rows)), response.BuildPagination(page, pageSize, total))
}

// GetProductSetting 获取当前用户的单个商品分销配置详情。
func (h *UserProductSettingHandler) GetProductSetting(c *gin.Context) {
	uid, ok := ginutil.GetUserID(c)
	if !ok {
		return
	}
	productID, err := ginutil.ParseParamUint(c, "product_id")
	if err != nil {
		ginutil.RespondError(c, response.CodeBadRequest, "error.bad_request", err)
		return
	}
	detail, err := h.service.GetUserProductSetting(uid, productID)
	if err != nil {
		respondUserProductSettingError(c, err, "error.user_fetch_failed")
		return
	}
	response.Success(c, dto.NewResellerProductSettingDetailResp(transportshared.DetailDTOInput(detail)))
}

// UpdateProductSettings 保存当前用户的商品级或 SKU 级分销配置。
func (h *UserProductSettingHandler) UpdateProductSettings(c *gin.Context) {
	uid, ok := ginutil.GetUserID(c)
	if !ok {
		return
	}
	productID, err := ginutil.ParseParamUint(c, "product_id")
	if err != nil {
		ginutil.RespondError(c, response.CodeBadRequest, "error.bad_request", err)
		return
	}
	var req transportshared.ProductSettingsUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ginutil.RespondBindError(c, err)
		return
	}
	input, err := req.ToInput()
	if err != nil {
		ginutil.RespondError(c, response.CodeBadRequest, "error.bad_request", err)
		return
	}
	detail, err := h.service.SaveUserProductSettings(uid, productID, input)
	if err != nil {
		respondUserProductSettingError(c, err, "error.save_failed")
		return
	}
	response.Success(c, dto.NewResellerProductSettingDetailResp(transportshared.DetailDTOInput(detail)))
}

// PreviewProductSettings 计算当前用户拟用定价规则的预计生效价与校验结果（不落库）。
func (h *UserProductSettingHandler) PreviewProductSettings(c *gin.Context) {
	uid, ok := ginutil.GetUserID(c)
	if !ok {
		return
	}
	productID, err := ginutil.ParseParamUint(c, "product_id")
	if err != nil {
		ginutil.RespondError(c, response.CodeBadRequest, "error.bad_request", err)
		return
	}
	var req transportshared.ProductSettingsUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ginutil.RespondBindError(c, err)
		return
	}
	input, err := req.ToInput()
	if err != nil {
		ginutil.RespondError(c, response.CodeBadRequest, "error.bad_request", err)
		return
	}
	items, err := h.service.PreviewUserProductSettings(uid, productID, input)
	if err != nil {
		respondUserProductSettingError(c, err, "error.user_fetch_failed")
		return
	}
	response.Success(c, dto.NewResellerProductSettingPreviewResp(transportshared.PreviewDTOInput(items)))
}

// ResetProductSetting 删除当前用户的商品级或 SKU 级分销配置。
func (h *UserProductSettingHandler) ResetProductSetting(c *gin.Context) {
	uid, ok := ginutil.GetUserID(c)
	if !ok {
		return
	}
	productID, err := ginutil.ParseParamUint(c, "product_id")
	if err != nil {
		ginutil.RespondError(c, response.CodeBadRequest, "error.bad_request", err)
		return
	}
	skuID, err := ginutil.ParseQueryUint(c.Query("sku_id"), false)
	if err != nil {
		ginutil.RespondError(c, response.CodeBadRequest, "error.bad_request", err)
		return
	}
	if err := h.service.ResetUserProductSetting(uid, productID, skuID); err != nil {
		respondUserProductSettingError(c, err, "error.save_failed")
		return
	}
	response.Success(c, gin.H{"deleted": true})
}
