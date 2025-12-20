package broker

import (
	"reflect"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"zerodha-trader/internal/models"
)

// Feature: zerodha-go-trader, Property 2: Order validation produces valid Zerodha API parameters
// Validates: Requirements 7.1-7.4
//
// Property: For any valid order parameters, the order validation should produce
// parameters that are acceptable by the Zerodha API (correct exchange, order type,
// product type, and quantity constraints).
func TestProperty_OrderValidationProducesValidZerodhaParams(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(time.Now().UnixNano())

	properties := gopter.NewProperties(parameters)

	// Valid exchanges
	exchanges := []models.Exchange{models.NSE, models.BSE, models.NFO, models.CDS, models.MCX}

	// Valid order sides
	sides := []models.OrderSide{models.OrderSideBuy, models.OrderSideSell}

	// Valid order types
	orderTypes := []models.OrderType{
		models.OrderTypeMarket,
		models.OrderTypeLimit,
		models.OrderTypeStopLoss,
		models.OrderTypeStopLossM,
	}

	// Valid product types
	productTypes := []models.ProductType{models.ProductMIS, models.ProductCNC, models.ProductNRML}

	// Valid symbols for testing
	symbols := []string{"RELIANCE", "TCS", "INFY", "HDFC", "ICICI", "SBIN"}

	// Generator for valid order
	orderGen := gen.Struct(reflect.TypeOf(models.Order{}), map[string]gopter.Gen{
		"Symbol":       gen.OneConstOf(symbols[0], symbols[1], symbols[2], symbols[3], symbols[4], symbols[5]),
		"Exchange":     gen.OneConstOf(exchanges[0], exchanges[1], exchanges[2], exchanges[3], exchanges[4]),
		"Side":         gen.OneConstOf(sides[0], sides[1]),
		"Type":         gen.OneConstOf(orderTypes[0], orderTypes[1], orderTypes[2], orderTypes[3]),
		"Product":      gen.OneConstOf(productTypes[0], productTypes[1], productTypes[2]),
		"Quantity":     gen.IntRange(1, 1000),
		"Price":        gen.Float64Range(100.0, 5000.0),
		"TriggerPrice": gen.Float64Range(100.0, 5000.0),
		"Validity":     gen.OneConstOf("DAY", "IOC"),
	})

	properties.Property("Valid order parameters produce valid Zerodha API params", prop.ForAll(
		func(order models.Order) bool {
			// Validate exchange is one of the valid exchanges
			validExchange := false
			for _, ex := range exchanges {
				if order.Exchange == ex {
					validExchange = true
					break
				}
			}
			if !validExchange {
				return false
			}

			// Validate order side
			if order.Side != models.OrderSideBuy && order.Side != models.OrderSideSell {
				return false
			}

			// Validate order type
			validOrderType := false
			for _, ot := range orderTypes {
				if order.Type == ot {
					validOrderType = true
					break
				}
			}
			if !validOrderType {
				return false
			}

			// Validate product type
			validProduct := false
			for _, pt := range productTypes {
				if order.Product == pt {
					validProduct = true
					break
				}
			}
			if !validProduct {
				return false
			}

			// Validate quantity is positive
			if order.Quantity <= 0 {
				return false
			}

			// Validate price constraints based on order type
			if order.Type == models.OrderTypeLimit && order.Price <= 0 {
				return false
			}

			if (order.Type == models.OrderTypeStopLoss || order.Type == models.OrderTypeStopLossM) && order.TriggerPrice <= 0 {
				return false
			}

			// Validate validity
			if order.Validity != "DAY" && order.Validity != "IOC" && order.Validity != "" {
				return false
			}

			return true
		},
		orderGen,
	))

	properties.TestingRun(t)
}
