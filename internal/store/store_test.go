package store_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/onuigboprecious/infarbloom/backend/internal/models"
	"github.com/onuigboprecious/infarbloom/backend/internal/store"
)

func TestSendOrderShippedEmail(t *testing.T) {
	// Should return safely without panic when RESEND_API_KEY is not set
	store.SendOrderShippedEmail("customer@example.com", "Favour Umeh", "972995", "Stealth Matte Black", 1, 19000, "Ajegule GRA", "Lagos", "TRK123456")

	// Call with handle and avatar parameters
	store.SendOrderShippedEmail("customer@example.com", "Favour Umeh", "972995", "Stealth Matte Black", 1, 19000, "Ajegule GRA", "Lagos", "TRK123456", "favourumeh", "https://example.com/avatar.png")

	// Empty email should return safely without panic
	store.SendOrderShippedEmail("", "Favour Umeh", "972995", "Stealth Matte Black", 1, 19000, "Ajegule GRA", "Lagos", "")
}

func TestUpdateOrderStatusToShipped(t *testing.T) {
	svc := store.New(nil)

	// 1. Create a test order
	orderReq := models.OrderRequest{
		FinishID:        "stealth-matte-black",
		FinishName:      "Stealth Matte Black",
		Quantity:        1,
		Amount:          19000,
		ShippingName:    "Favour Umeh",
		Phone:           "08065365632",
		Email:           "favourumeh1109@gmail.com",
		DeliveryAddress: "Ajegule GRA",
		City:            "Lagos",
		PaymentRef:      "ref_test_123",
	}

	orderBody, _ := json.Marshal(orderReq)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewBuffer(orderBody))
	req.Header.Set("Content-Type", "application/json")
	svc.HandleOrders(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created on order creation, got %d", rec.Code)
	}

	var res map[string]interface{}
	_ = json.NewDecoder(rec.Body).Decode(&res)
	orderData, ok := res["order"].(map[string]interface{})
	if !ok {
		t.Fatalf("failed to decode order from response: %+v", res)
	}
	orderID := orderData["id"].(string)

	// 2. Update order status to "shipped"
	updatePayload, _ := json.Marshal(map[string]string{
		"status": "shipped",
	})
	updateRec := httptest.NewRecorder()
	updateReq := httptest.NewRequest(http.MethodPatch, "/api/admin/orders/"+orderID+"/status", bytes.NewBuffer(updatePayload))
	updateReq.SetPathValue("id", orderID)
	updateReq.Header.Set("Content-Type", "application/json")
	svc.HandleUpdateOrderStatus(updateRec, updateReq)

	if updateRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on update order status, got %d", updateRec.Code)
	}
}
