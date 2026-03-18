package utils

import (
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"
)

var skipColumns = map[string]bool{
	"id":              true,
	"user_created_at": true,
}

var validSortFields = map[string]string{
	"id":               "id",
	"agent_code":       "agent_code",
	"title":            "title",
	"views":            "views",
	"description":      "description",
	"price":            "price",
	"average_rating":   "average_rating",
	"rating_quantity":  "rating_quantity",
	"address":          "address",
	"property_id":      "property_id",
	"city":             "city",
	"state":            "state",
	"location":         "location",
	"school_nearby":    "school_nearby",
	"property_type":    "property_type",
	"amenities":        "amenities",
	"images":           "images",
	"property_contact": "property_contact",
	"status":           "status",
	"is_approved":      "is_approved",
	"created_at":       "created_at",
	"updated_at":       "updated_at",
}

// FilterConfig defines how to handle each filter parameter
type FilterConfig struct {
	DBColumn string
	Operator string // "=", ">", "<", ">=", "<=", "LIKE", "ILIKE"
	IsString bool   // For LIKE queries, wrap in %
}

var validFilters = map[string]FilterConfig{
	// Exact match filters
	"id":            {DBColumn: "id", Operator: "="},
	"agent_code":    {DBColumn: "a.agent_code", Operator: "="},
	"property_id":   {DBColumn: "property_id", Operator: "="},
	"city":          {DBColumn: "city", Operator: "="},
	"state":         {DBColumn: "state", Operator: "="},
	"property_type": {DBColumn: "property_type", Operator: "="},
	"status":        {DBColumn: "status", Operator: "="},
	"is_approved":   {DBColumn: "is_approved", Operator: "="},
	"school_nearby": {DBColumn: "school_nearby", Operator: "="},

	// Range filters - Price
	"min_price": {DBColumn: "price", Operator: ">="},
	"max_price": {DBColumn: "price", Operator: "<="},
	"price":     {DBColumn: "price", Operator: "="},

	// Range filters - Views
	"min_views": {DBColumn: "views", Operator: ">="},
	"max_views": {DBColumn: "views", Operator: "<="},
	"views":     {DBColumn: "views", Operator: "="},

	// Range filters - Rating
	"min_rating":     {DBColumn: "average_rating", Operator: ">="},
	"max_rating":     {DBColumn: "average_rating", Operator: "<="},
	"average_rating": {DBColumn: "average_rating", Operator: "="},

	// Range filters - Rating Quantity
	"min_rating_quantity": {DBColumn: "rating_quantity", Operator: ">="},
	"max_rating_quantity": {DBColumn: "rating_quantity", Operator: "<="},
	"rating_quantity":     {DBColumn: "rating_quantity", Operator: "="},

	// Text search filters
	"title_search":       {DBColumn: "title", Operator: "ILIKE", IsString: true},
	"description_search": {DBColumn: "description", Operator: "ILIKE", IsString: true},
	"address_search":     {DBColumn: "address", Operator: "ILIKE", IsString: true},

	// Date range filters
	"created_after":  {DBColumn: "created_at", Operator: ">="},
	"created_before": {DBColumn: "created_at", Operator: "<="},
	"updated_after":  {DBColumn: "updated_at", Operator: ">="},
	"updated_before": {DBColumn: "updated_at", Operator: "<="},
}

func AddSorting(r *http.Request, query string) string {
	sortParams := r.URL.Query()["sortby"]
	if len(sortParams) == 0 {
		return query + " ORDER BY created_at DESC"
	}

	var validSorts []string
	for _, param := range sortParams {
		parts := strings.Split(param, ":")
		if len(parts) != 2 {
			continue
		}

		field, order := parts[0], strings.ToLower(parts[1])

		dbColumn, validField := validSortFields[field]
		if !validField {
			continue
		}

		if order != "asc" && order != "desc" {
			continue
		}

		validSorts = append(validSorts, fmt.Sprintf("%s %s", dbColumn, strings.ToUpper(order)))
	}

	if len(validSorts) > 0 {
		query += " ORDER BY " + strings.Join(validSorts, ", ")
	} else {
		query += " ORDER BY created_at DESC"
	}

	return query
}

func AddFilters(r *http.Request, query string, args []interface{}) (string, []interface{}) {
	hasWhere := strings.Contains(strings.ToLower(query), "where")

	for param, config := range validFilters {
		value := r.URL.Query().Get(param)
		if value == "" {
			continue
		}

		if hasWhere {
			query += " AND "
		} else {
			query += " WHERE "
			hasWhere = true
		}

		placeholderNum := len(args) + 1
		query += fmt.Sprintf("%s %s $%d", config.DBColumn, config.Operator, placeholderNum)

		if config.IsString && (config.Operator == "LIKE" || config.Operator == "ILIKE") {
			args = append(args, "%"+value+"%")
		} else {
			args = append(args, convertValue(param, value))
		}
	}

	return query, args
}

func convertValue(param, value string) interface{} {
	if param == "is_approved" {
		if b, err := strconv.ParseBool(value); err == nil {
			return b
		}
		return value
	}

	intFields := map[string]bool{
		"id":                  true,
		"agent_code":          true,
		"views":               true,
		"min_views":           true,
		"max_views":           true,
		"rating_quantity":     true,
		"min_rating_quantity": true,
		"max_rating_quantity": true,
	}
	if intFields[param] {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}

	floatFields := map[string]bool{
		"average_rating": true,
		"min_rating":     true,
		"max_rating":     true,
	}
	if floatFields[param] {
		if f, err := strconv.ParseFloat(value, 64); err == nil {
			return f
		}
	}

	priceFields := map[string]bool{
		"price":     true,
		"min_price": true,
		"max_price": true,
	}
	if priceFields[param] {
		return value
	}

	dateFields := map[string]bool{
		"created_after":  true,
		"created_before": true,
		"updated_after":  true,
		"updated_before": true,
	}
	if dateFields[param] {
		formats := []string{
			time.RFC3339,
			"2006-01-02",
			"2006-01-02 15:04:05",
		}
		for _, format := range formats {
			if t, err := time.Parse(format, value); err == nil {
				return t
			}
		}
	}

	return value
}

func GenerateInsertQuery(tableName string, model interface{}) string {
	modelValue := reflect.ValueOf(model)
	modelType := modelValue.Type()

	var columns []string
	var placeholders []string
	placeholderNum := 1

	for i := 0; i < modelType.NumField(); i++ {
		field := modelType.Field(i)
		dbTag := strings.TrimSuffix(field.Tag.Get("db"), ",omitempty")
		if dbTag == "" || skipColumns[dbTag] {
			continue
		}

		// val := modelValue.Field(i).Interface()

		// if ns, ok := val.(sql.NullString); ok && !ns.Valid {
		// 	continue
		// }

		columns = append(columns, dbTag)
		placeholders = append(placeholders, fmt.Sprintf("$%d", placeholderNum))
		placeholderNum++
	}

	return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		tableName,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "))
}

func GetStructValues(model interface{}) []interface{} {
	modelValue := reflect.ValueOf(model)
	modelType := modelValue.Type()
	values := []interface{}{}

	for i := 0; i < modelType.NumField(); i++ {
		field := modelType.Field(i)
		dbTag := strings.TrimSuffix(field.Tag.Get("db"), ",omitempty")
		if dbTag == "" || skipColumns[dbTag] {
			continue
		}

		val := modelValue.Field(i).Interface()

		if str, ok := val.(string); ok {
			if str == "" {
				values = append(values, nil)
				continue
			}
		}

		values = append(values, val)
	}

	return values
}
