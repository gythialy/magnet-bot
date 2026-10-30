package main

import (
	"log"
	"strings"

	"github.com/glebarez/sqlite"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"gorm.io/gen"
	"gorm.io/gen/field"
	"gorm.io/gorm"
)

func main() {
	// Initialize the generator with configuration
	g := gen.NewGenerator(gen.Config{
		OutPath:           "pkg/dal", // output directory, default value is ./query
		ModelPkgPath:      "pkg/model",
		WithUnitTest:      true,
		FieldNullable:     true,
		FieldCoverable:    false,
		FieldSignable:     false,
		FieldWithIndexTag: true,
		FieldWithTypeTag:  false,
		Mode:              gen.WithDefaultQuery | gen.WithQueryInterface | gen.WithoutContext,
	})

	// Initialize a *gorm.DB instance
	db, err := gorm.Open(sqlite.Open("bot.db"), &gorm.Config{})
	if err != nil {
		log.Fatalf("open bot.db: %v", err)
	}
	g.UseDB(db)

	caser := cases.Title(language.English)
	tagWithNS := gen.FieldJSONTagWithNS(func(columnName string) string {
		// Convert snake_case to camelCase
		parts := strings.Split(columnName, "_")
		for i := 1; i < len(parts); i++ {
			parts[i] = caser.String(parts[i])
		}
		return strings.Join(parts, "")
	})
	g.ApplyBasic(
		g.GenerateModel("alarms", gen.FieldType("user_id", "int64"),
			gen.FieldGORMTag("user_id", func(tag field.GormTag) field.GormTag {
				tag.Set("primaryKey", "").Append("autoIncrement", "false")
				return tag
			}),
			gen.FieldGORMTag("credit_code", func(tag field.GormTag) field.GormTag {
				tag.Set("primaryKey", "").Append("autoIncrement", "false").
					Append("not null")
				return tag
			}),
			tagWithNS),
		g.GenerateModel("histories", gen.FieldType("user_id", "int64"),
			gen.FieldGORMTag("user_id", func(tag field.GormTag) field.GormTag {
				tag.Set("primaryKey", "").Append("autoIncrement", "false")
				return tag
			}), gen.FieldGORMTag("url", func(tag field.GormTag) field.GormTag {
				tag.Set("primaryKey", "")
				return tag
			}),
			gen.FieldGORMTag("has_tender_code", func(tag field.GormTag) field.GormTag {
				tag.Append("not null").Append("default", "0")
				return tag
			}),
			tagWithNS),
		g.GenerateModel("keywords", gen.FieldType("user_id", "int64"), tagWithNS),
	)

	// Execute the generator
	g.Execute()
}
