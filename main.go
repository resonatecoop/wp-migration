package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/pariz/gountries"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v4"
	stdlib "github.com/jackc/pgx/v4/stdlib"
	wpmodel "github.com/resonatecoop/user-api/legacy_wp_model"
	"github.com/resonatecoop/user-api/model"
	pgmodel "github.com/resonatecoop/user-api/model"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/mysqldialect"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/extra/bundebug"
)

func main() {
	var (
		err                error
		ctx                context.Context
		targetPSDB         *bun.DB
		sourceWPDB         *bun.DB
		wpusers            []wpmodel.WpUser
		pgusers            []pgmodel.User
		allEmails          []string
		allNicknames       []string
		role_id            int32
		inserted           int    = 0
		updated            int    = 0
		skipped            int    = 0
		postgresDBUser     string = "resonate_test_user"
		postgresDBPassword string = "password"
		postgresDBName     string = "resonate_test"
		postgresDBHost     string = "127.0.0.1"
		postgresDBPort     string = "5432"
		mysqlDBUser        string = "resonate_is"
		mysqlDBPassword    string = ""
		mysqlDBName        string = "resonate_is"
		mysqlDBHost        string = "127.0.0.1"
		mysqlDBPort        string = "3306"
	)

	err = godotenv.Load()

	if err != nil {
		log.Fatal("Error loading .env file")
	}

	// postgres db config
	if os.Getenv("POSTGRES_DB_USER") != "" {
		postgresDBUser = os.Getenv("POSTGRES_DB_USER")
	}

	if os.Getenv("POSTGRES_DB_PASSWORD") != "" {
		postgresDBPassword = os.Getenv("POSTGRES_DB_PASSWORD")
	}

	if os.Getenv("POSTGRES_DB_HOST") != "" {
		postgresDBHost = os.Getenv("POSTGRES_DB_HOST")
	}

	if os.Getenv("POSTGRES_DB_PORT") != "" {
		postgresDBPort = os.Getenv("POSTGRES_DB_PORT")
	}

	if os.Getenv("POSTGRES_DB_NAME") != "" {
		postgresDBName = os.Getenv("POSTGRES_DB_NAME")
	}

	// mysql db config
	if os.Getenv("MYSQL_DB_USER") != "" {
		mysqlDBUser = os.Getenv("MYSQL_DB_USER")
	}

	if os.Getenv("MYSQL_DB_PASSWORD") != "" {
		mysqlDBPassword = os.Getenv("MYSQL_DB_PASSWORD")
	}

	if os.Getenv("MYSQL_DB_HOST") != "" {
		mysqlDBHost = os.Getenv("MYSQL_DB_HOST")
	}

	if os.Getenv("MYSQL_DB_PORT") != "" {
		mysqlDBPort = os.Getenv("MYSQL_DB_PORT")
	}

	if os.Getenv("MYSQL_DB_NAME") != "" {
		mysqlDBName = os.Getenv("MYSQL_DB_NAME")
	}

	ctx = context.Background()
	targetPSDB = connectPSDB(postgresDBUser, postgresDBPassword, postgresDBHost, postgresDBPort, postgresDBName, true)
	sourceWPDB = connectWPDB(mysqlDBUser, mysqlDBPassword, mysqlDBHost, mysqlDBPort, mysqlDBName, true)

	err = sourceWPDB.NewSelect().
		Model(&wpusers).
		Where("user_email NOT LIKE ?", "%@resonate.is").
		Scan(ctx)

	if err != nil {
		panic(err)
	}

	_, err = fmt.Println("Number of WP users:", len(wpusers))

	if err != nil {
		panic(err)
	}

	err = targetPSDB.NewSelect().
		Model(&pgusers).
		Scan(ctx)

	if err != nil {
		panic(err)
	}

	_, err = fmt.Println("Number of PG users:", len(pgusers))

	if err != nil {
		panic(err)
	}

	for _, thisUser := range wpusers {

		if thisUser.Email == "" {
			fmt.Println("User with blank email skipped, id: ", thisUser.ID)
			skipped++
			continue
		}
		if Seen(allEmails, thisUser.Email) {
			fmt.Println("User with duplicate email skipped, id: ", thisUser.ID)
			skipped++
			continue
		}

		allEmails = append(allEmails, thisUser.Email)

		thisUsersRole, err := getUserMetaValue(sourceWPDB, ctx, &thisUser, "role")

		if err != nil {
			role_id = 6
		}

		switch thisUsersRole {
		case "member":
			role_id = 5
		case "label-owner":
			role_id = 4
		case "admin":
			role_id = 3
		default:
			role_id = 6
		}

		newPGUser := &model.User{
			Username: thisUser.Email,
			RoleID:   role_id,
			LegacyID: int32(thisUser.ID),
			Password: thisUser.Password,
			TenantID: 0,
		}

		err = getTrack(sourceWPDB, ctx, &thisUser)

		if err == nil {
			newPGUser.Member = true
		}

		thisUsersCountry, err := getUserMetaValue(sourceWPDB, ctx, &thisUser, "country")

		if err == nil {
			query := gountries.New()

			gountry, _ := query.FindCountryByName(thisUsersCountry)

			newPGUser.Country = gountry.Codes.Alpha2
		}

		existingUser := new(model.User)

		err = targetPSDB.NewSelect().
			Model(existingUser).
			Where("username = ?", thisUser.Email).
			Limit(1).
			Scan(ctx)

		if err == nil {
			//update
			_, err = targetPSDB.NewUpdate().
				Model(newPGUser).
				Column("id", "username", "password", "legacy_id", "country", "role_id", "tenant_id", "member").
				Where("username = ?", thisUser.Email).
				Exec(ctx)

			if err != nil {
				panic(err)
			}

			updated++
		} else {
			//insert
			_, err = targetPSDB.NewInsert().
				Model(newPGUser).
				Column("id", "username", "password", "legacy_id", "country", "role_id", "tenant_id", "member").
				Exec(ctx)

			if err != nil {
				panic(err)
			}

			inserted++
		}

		if role_id == 5 {
			//insert a new UserGroup

			var thisUsersNickname = ""

			thisUsersNickname, err := getUserMetaValue(sourceWPDB, ctx, &thisUser, "nickname")

			if err != nil {
				panic(err)
			}

			if thisUsersNickname != "" && !Seen(allNicknames, thisUsersNickname) {
				allNicknames = append(allNicknames, thisUsersNickname)

				var refUserID uuid.UUID

				if newPGUser.ID == uuid.Nil {
					refUserID = existingUser.ID
				} else {
					refUserID = newPGUser.ID
				}

				newPGUserGroup := &model.UserGroup{
					OwnerID:     refUserID,
					DisplayName: thisUsersNickname,
				}

				err = targetPSDB.NewSelect().
					Model(newPGUserGroup).
					Where("owner_id = ?", refUserID).
					Scan(ctx)

				if err != nil {
					//insert
					_, err = targetPSDB.NewInsert().
						Model(newPGUserGroup).
						Exec(ctx)

					if err != nil {
						panic(err)
					}
				} else {
					//update
					_, err = targetPSDB.NewUpdate().
						Model(newPGUserGroup).
						Set("display_name = ?", thisUsersNickname).
						Where("owner_id = ?", refUserID).
						Exec(ctx)

					if err != nil {
						panic(err)
					}
				}

			}

		}
	}

	fmt.Println("FINISHED")
	fmt.Println("Users inserted: ", inserted)
	fmt.Println("Users updated: ", updated)
	fmt.Println("Users skipped: ", skipped)

	err = targetPSDB.NewSelect().
		Model(&pgusers).
		Scan(ctx)

	if err != nil {
		panic(err)
	}

	fmt.Println("Number of PG users:", len(pgusers))
}

// Need track model on user-api legacy
func getTrack(WPDB *bun.DB, ctx context.Context, user *wpmodel.WpUser) error {
	var (
		err error
	)

	status := []int{0, 2, 3}

	track := map[string]interface{}{}

	err = WPDB.NewSelect().
		Model(&track).
		Table("tracks").
		Where("uid = ?", user.ID).
		Where("status IN (?)", bun.In(status)).
		Scan(ctx)

	if err != nil {
		return err
	}

	return nil
}

func getUserMetaValue(WPDB *bun.DB, ctx context.Context, user *wpmodel.WpUser, key string) (string, error) {
	var (
		err error
	)

	userMeta := new(wpmodel.WpUserMeta)

	err = WPDB.NewSelect().
		Model(userMeta).
		Where("meta_key = ?", key).
		Where("user_id = ?", user.ID).
		Scan(ctx)

	if err != nil {
		return "", err
	}

	return userMeta.MetaValue, nil
}

func Seen(list []string, item string) bool {

	var result bool = false
	for _, x := range list {
		if x == item {
			result = true
			break
		}
	}

	return result
}

func connectPSDB(username string, password string, host string, port string, dbname string, isDebug bool) *bun.DB {

	dbconfig, err := pgx.ParseConfig(
		fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", username, password, host, port, dbname),
	)

	if err != nil {
		panic(err)
	}

	dbconfig.PreferSimpleProtocol = true

	sqldb := stdlib.OpenDB(*dbconfig)

	db := bun.NewDB(sqldb, pgdialect.New())
	if isDebug {
		db.AddQueryHook(bundebug.NewQueryHook(bundebug.WithVerbose()))
	}

	return db
}

func connectWPDB(username string, password string, host string, port string, dbname string, isDebug bool) *bun.DB {

	sqldb, err := sql.Open("mysql", fmt.Sprintf("%s:%s@(%s:%s)/%s", username, password, host, port, dbname))
	if err != nil {
		panic(err)
	}

	db := bun.NewDB(sqldb, mysqldialect.New())
	if isDebug {
		db.AddQueryHook(bundebug.NewQueryHook(bundebug.WithVerbose()))
	}

	return db
}
