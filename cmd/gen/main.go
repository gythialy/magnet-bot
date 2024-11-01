package main

import (
	"strings"

	"github.com/glebarez/sqlite"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"gorm.io/gen"
	"gorm.io/gen/field"
	"gorm.io/gorm"
)

type HistoryQuerier interface {
	// SELECT COUNT(*) FROM @@table WHERE user_id = @userId AND url = @url
	IsUrlExist(userId int, url string) (gen.RowsAffected, error)
}

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
	db, _ := gorm.Open(sqlite.Open("bot.db"), &gorm.Config{})
	//
	// Use the above `*gorm.DB` instance to initialize the generator,
	// which is required to generate structs from db when using `GenerateModel/GenerateModelAs`
	g.UseDB(db)

	// Generate default DAO interface for those specified structs
	// g.ApplyBasic(entities.Keyword{}, entities.Alarm{}, entities.History{})
	// g.ApplyInterface(func(HistoryQuerier) {}, entities.History{})

	// Generate default DAO interface for those generated structs from database
	// companyGenerator := g.GenerateModelAs("company", "MyCompany")
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
				tag.Set("primaryKey", "").Append("autoIncrement", "false")
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
			tagWithNS),
		g.GenerateModel("keywords", gen.FieldType("user_id", "int64"),
			//gen.FieldGORMTag("id", func(tag field.GormTag) field.GormTag {
			//	tag.Set("primaryKey", "").Append("autoIncrement", "true")
			//	return tag
			//}),
			tagWithNS),
		//companyGenerator,
		//g.GenerateModelAs("people", "Person",
		//	gen.FieldIgnore("deleted_at"),
		//	gen.FieldNewTag("age", `json:"-"`),
		//),
	)

	// Execute the generator
	g.Execute()
}
