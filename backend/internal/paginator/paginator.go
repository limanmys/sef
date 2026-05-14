package paginator

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

var validColumnName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

type Filter struct {
	Key       string `json:"id"`
	Value     any    `json:"value"`
	Operator  string `json:"operator"`
	LastValue any    `json:"last_value"`
}

type Paginator struct {
	DB      *gorm.DB
	OrderBy []string
	Page    int
	PerPage int
	Filter  []Filter
}

type Data struct {
	TotalRecords int64       `json:"total_records"`
	Records      interface{} `json:"records"`
	CurrentPage  int         `json:"current_page"`
	TotalPages   int         `json:"total_pages"`
}

func New(tx *gorm.DB, c fiber.Ctx) *Paginator {
	return &Paginator{
		DB:      tx,
		OrderBy: []string{convertOrder(getKey("sort", "-updated_at", c))},
		Page:    getIntKey("page", 1, c),
		PerPage: getIntKey("per_page", 25, c),
		Filter:  getFilters("filter", c),
	}
}

func NewNoFilter(tx *gorm.DB, c fiber.Ctx) *Paginator {
	return &Paginator{
		DB:      tx,
		OrderBy: []string{convertOrder(getKey("sort", "-updated_at", c))},
		Page:    getIntKey("page", 1, c),
		PerPage: getIntKey("per_page", 25, c),
		Filter:  nil,
	}
}

func NewnoOrder(tx *gorm.DB, c fiber.Ctx) *Paginator {
	return &Paginator{
		DB:      tx,
		OrderBy: nil,
		Page:    getIntKey("page", 1, c),
		PerPage: getIntKey("per_page", 25, c),
		Filter:  getFilters("filter", c),
	}
}

func NewSpecificOrder(tx *gorm.DB, c fiber.Ctx, key string) *Paginator {
	return &Paginator{
		DB:      tx,
		OrderBy: []string{convertOrder(getKey("sort", key, c))},
		Page:    getIntKey("page", 1, c),
		PerPage: getIntKey("per_page", 25, c),
		Filter:  getFilters("filter", c),
	}
}

func iterate(db *gorm.DB, v reflect.Type, filter Filter) {
	// Dereference pointer if needed
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return // Skip if not a struct
	}

	for fieldIterator := 0; fieldIterator < v.NumField(); fieldIterator++ {
		// We need this check for invalid filter keys
		type_name := v.Field(fieldIterator).Type.Name()
		switch v.Field(fieldIterator).Type.Kind() {
		case reflect.Struct:
			if type_name == "Time" && v.Field(fieldIterator).Tag.Get("json") == filter.Key {
				db = db.Where(
					"LOWER(to_char("+filter.Key+", 'YYYY/MM/DD, DAY, DD MONTH YYYY HH:MM:SS')) LIKE LOWER(?)",
					fmt.Sprintf("%%%s%%", filter.Value),
				)
			} else {
				iterate(db, v.Field(fieldIterator).Type, filter)
			}

		default:
			if v.Field(fieldIterator).Tag.Get("json") == filter.Key {
				rt := reflect.TypeOf(filter.Value)
				switch rt.Kind() {
				case reflect.Slice:
					db = db.Where(
						filter.Key+" IN ?",
						filter.Value.([]interface{}),
					)
				default:

					switch filter.Operator {
					case "", "equal":
						// Handle "null" value for IS NULL checks
						if filter.Value == "null" {
							db = db.Where(filter.Key + " IS NULL")
						} else {
							db = db.Where(
								"LOWER("+filter.Key+"::text) LIKE LOWER(?)",
								fmt.Sprintf("%%%s%%", filter.Value),
							)
						}
					case "less":
						db = db.Where(
							"SUBSTRING(LOWER("+filter.Key+"::text),1, ?) < LOWER(?::text)",
							len(filter.Value.(string)), filter.Value,
						)
					case "less equal":
						db = db.Where(
							"SUBSTRING(LOWER("+filter.Key+"::text),1, ?) <= LOWER(?::text)",
							len(filter.Value.(string)), filter.Value,
						)
					case "greater":
						val := "0" + filter.Value.(string)
						db = db.Where(
							"CONCAT('0',SUBSTRING(LOWER("+filter.Key+"::text),1, ?)) > LOWER(?::text)",
							len(val), val,
						)
					case "greater equal":
						val := "0" + filter.Value.(string)
						db = db.Where(
							"CONCAT('0',SUBSTRING(LOWER("+filter.Key+"::text),1, ?)) >= LOWER(?::text)",
							len(val), val,
						)
					case "between":
						val := "0" + filter.Value.(string)
						db = db.Where(
							"SUBSTRING(LOWER("+filter.Key+"::text),1, ?) > LOWER(?::text) AND SUBSTRING(LOWER("+filter.Key+"::text),1, ?) < LOWER(?::text)",
							len(val), filter.Value, len(filter.LastValue.(string)), filter.LastValue,
						)
					case "not equal":
						db = db.Where(
							"LOWER("+filter.Key+"::text) NOT LIKE LOWER(?)",
							fmt.Sprintf("%%%s%%", filter.Value),
						)

					default:
						db = db.Where(
							"LOWER("+filter.Key+"::text) LIKE LOWER(?)",
							fmt.Sprintf("%%%s%%", filter.Value),
						)
					}

				}
			}
		}
	}
}

func (p *Paginator) Paginate(dataSource interface{}) (*Data, error) {
	db := p.DB

	if len(p.OrderBy) > 0 {
		validCols := modelJSONColumns(dataSource)
		for _, o := range p.OrderBy {
			if o == "" {
				continue
			}
			col := strings.Fields(o)[0]
			if validCols[col] {
				db = db.Order(o)
			}
		}
	}

	var output Data
	var count int64
	var offset int

	for _, filter := range p.Filter {
		// We do this because of the type of datasource is not model, it's []model
		// Double elem is used to get the model type
		v := reflect.TypeOf(dataSource).Elem().Elem()

		if v.String() != "map[string]interface {}" {
			iterate(db, v, filter)
		} else {
			rt := reflect.TypeOf(filter.Value)

			switch rt.Kind() {
			case reflect.Slice:
				db = db.Where(
					filter.Key+" IN ?",
					filter.Value.([]interface{}),
				)
			default:
				switch filter.Operator {
				case "", "equal":
					// Handle "null" value for IS NULL checks
					if filter.Value == "null" {
						db = db.Where(filter.Key + " IS NULL")
					} else {
						db = db.Where(
							"LOWER("+filter.Key+"::text) LIKE LOWER(?)",
							fmt.Sprintf("%%%s%%", filter.Value),
						)
					}
				case "less":
					db = db.Where(
						"SUBSTRING(LOWER("+filter.Key+"::text),1, ?) < LOWER(?::text)",
						len(filter.Value.(string)), filter.Value,
					)
				case "less equal":
					db = db.Where(
						"SUBSTRING(LOWER("+filter.Key+"::text),1, ?) <= LOWER(?::text)",
						len(filter.Value.(string)), filter.Value,
					)
				case "greater":
					val := "0" + filter.Value.(string)
					db = db.Where(
						"CONCAT('0',SUBSTRING(LOWER("+filter.Key+"::text),1, ?)) > LOWER(?::text)",
						len(val), val,
					)
				case "greater equal":
					val := "0" + filter.Value.(string)
					db = db.Where(
						"CONCAT('0',SUBSTRING(LOWER("+filter.Key+"::text),1, ?)) >= LOWER(?::text)",
						len(val), val,
					)
				case "between":
					val := "0" + filter.Value.(string)
					db = db.Where(
						"SUBSTRING(LOWER("+filter.Key+"::text),1, ?) > LOWER(?::text) AND SUBSTRING(LOWER("+filter.Key+"::text),1, ?) < LOWER(?::text)",
						len(val), filter.Value, len(filter.LastValue.(string)), filter.LastValue,
					)
				case "not equal":
					db = db.Where(
						"LOWER("+filter.Key+"::text) NOT LIKE LOWER(?)",
						fmt.Sprintf("%%%s%%", filter.Value),
					)

				default:
					db = db.Where(
						"LOWER("+filter.Key+"::text) LIKE LOWER(?)",
						fmt.Sprintf("%%%s%%", filter.Value),
					)
				}
			}
		}
	}

	tmp := db.Statement.Preloads
	db.Statement.Preloads = map[string][]interface{}{}
	err := db.Model(dataSource).Count(&count).Error
	db.Statement.Preloads = tmp

	if err != nil {
		return nil, err
	}

	if p.Page == 1 {
		offset = 0
	} else {
		offset = (p.Page - 1) * p.PerPage
	}

	err = db.Limit(p.PerPage).Offset(offset).Find(dataSource).Error
	if err != nil {
		return nil, err
	}

	output.TotalRecords = count
	output.Records = dataSource
	output.CurrentPage = p.Page
	output.TotalPages = getTotalPages(p.PerPage, count)

	return &output, nil
}

func getTotalPages(perPage int, totalRecords int64) int {
	return int(math.Ceil(float64(totalRecords) / float64(perPage)))
}

func getKey(k string, d string, c fiber.Ctx) string {
	if c.FormValue(k) != "" {
		return c.FormValue(k)
	}
	return d
}

func getIntKey(k string, d int, c fiber.Ctx) int {
	key := getKey(k, fmt.Sprintf("%d", d), c)
	value, err := strconv.Atoi(key)
	if err != nil {
		return d
	}
	return value
}

// modelJSONColumns returns a set of json tag names from the model's struct fields.
// Only these names are allowed as sort columns.
func modelJSONColumns(dataSource interface{}) map[string]bool {
	t := reflect.TypeOf(dataSource)
	for t.Kind() == reflect.Ptr || t.Kind() == reflect.Slice {
		t = t.Elem()
	}
	cols := make(map[string]bool)
	collectJSONColumns(t, cols)
	return cols
}

func collectJSONColumns(t reflect.Type, cols map[string]bool) {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := strings.SplitN(f.Tag.Get("json"), ",", 2)[0]
		if tag != "" && tag != "-" {
			cols[tag] = true
		}
		if f.Anonymous || (f.Type.Kind() == reflect.Struct) {
			collectJSONColumns(f.Type, cols)
		}
	}
}

func convertOrder(order string) string {
	dir := " asc"
	col := order

	if len(order) == 0 {
		return ""
	}

	if order[0] == '-' {
		dir = " desc"
		col = order[1:]
	} else if order[0] == '+' {
		col = order[1:]
	}

	if !validColumnName.MatchString(col) {
		return ""
	}

	return col + dir
}

func getFilters(field_name string, c fiber.Ctx) []Filter {
	filters := []Filter{}
	if c.Query(field_name) != "" {
		json.Unmarshal(
			[]byte(strings.TrimSpace(c.Query(field_name))),
			&filters,
		)
	}

	return filters
}

// TODO: put it in utils
func GetFilters(field_name string, c fiber.Ctx) []Filter {
	return getFilters(field_name, c)
}
