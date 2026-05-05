package cli

func (app *App) validateSymbol(symbol string) error {
	if app.Config == nil || !app.Config.Security.StrictValidation || app.Validator == nil {
		return nil
	}
	return app.Validator.ValidateSymbol(symbol)
}

func (app *App) validateOrderID(orderID string) error {
	if app.Config == nil || !app.Config.Security.StrictValidation || app.Validator == nil {
		return nil
	}
	return app.Validator.ValidateOrderID(orderID)
}

func (app *App) validateWatchlistName(name string) error {
	if app.Config == nil || !app.Config.Security.StrictValidation || app.Validator == nil {
		return nil
	}
	return app.Validator.ValidateWatchlistName(name)
}

func (app *App) validateQuantity(qty int) error {
	if app.Validator == nil {
		return nil
	}
	return app.Validator.ValidateQuantity(qty)
}

func (app *App) validatePrice(price float64) error {
	if app.Validator == nil {
		return nil
	}
	return app.Validator.ValidatePrice(price)
}

func (app *App) validateText(field, text string, maxLen int) error {
	if app.Validator == nil {
		return nil
	}
	return app.Validator.ValidateText(field, text, maxLen)
}
