package main

import (
	"database/sql"
	"fmt"
	"math"
	"html/template"
	"log"
	"time"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"errors" 

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// SolarEquipment - Первая модель-коллекция (Услуги)
type SolarEquipment struct {
	ID            int     `gorm:"primaryKey;column:id"`
	ModelName     string  `gorm:"column:model_name"`
	Price         float64 `gorm:"column:price"`
	Type          string  `gorm:"column:type";default:'power'"` // "power" или "capacity""`
	Power         int     `gorm:"column:power"`
	Capacity      int     `gorm:"column:capacity"`
	Description   string  `gorm:"column:description"`
	ImageKey      string  `gorm:"column:image_key"`
	VideoKey      string  `gorm:"column:video_key"`
	Status        string  `gorm:"column:status;default:'active'"` // "active" или "deleted""`
}

func (SolarEquipment) TableName() string {
	return "solar_equipments"
}

// SolarRequest - Вторая модель-коллекция (Заявки)
type SolarRequest struct {
	ID            int       `gorm:"primaryKey;column:id"`
	UserID        int       `gorm:"column:user_id"`     // пользователь
	CreatedAt     time.Time `gorm:"column:created_at"`  // время и дата создания
	Latitude      float64   `gorm:"column:latitude"`    // широта места установки
	Insolation    float64   `gorm:"column:insolation"`  // расчет 
	Status        string    `gorm:"column:status;default:'draft'"` // "draft," или "deleted""`
}

func (SolarRequest) TableName() string {
	return "solar_requests"
}

// SolarRequestStr - Третья модель-коллекция (Состав заявки)
type SolarRequestStr struct {
	ID          uint   `gorm:"primaryKey"`                    // ID записи в БД
	// Задаем общее имя индекса idx_req_eq_status для всех трех полей
	RequestID   int    `gorm:"uniqueIndex:idx_req_eq_status"` // ID заявки
	EquipmentID int    `gorm:"uniqueIndex:idx_req_eq_status"` // ID товара
	Status      string `gorm:"uniqueIndex:idx_req_eq_status"` // "draft," или "deleted""`
	Quantity    int    `gorm:"column:quantity"`               // Количество
}

func (SolarRequestStr) TableName() string {
	return "solar_request_strs"
}

// SolarUsers - Четвертая модель-коллекция (Пользователи)
type SolarUsers struct {
	ID     int    `gorm:"primaryKey;column:id"`           // ID записи в БД
	Name   string `gorm:"column:name"`                    // наименование
	Role   string `gorm:"column:role;default:'guest'"`    // "guest", "client", "moderator", "service" или "deleted" (Требование ЛР2)
	Status string `gorm:"column:status;default:'active'"` // "active" или "deleted""`
}

func (SolarUsers) TableName() string {
	return "solar_users"
}

// SolarInsolation - Пятая модель-коллекция для расчета выработки кВт*ч(Инсоляция)
type SolarInsolation struct {
	gorm.Model
	Latitude float64 `gorm:"uniqueIndex;not null"`   // широта места установки
	KwhM2    float64 `gorm:"column:kwh_m2;not null"` // годовая инсоляция кВч/м2
}

func (SolarInsolation) TableName() string {
	return "solar_insolation"
}

// Структура для отображения элементов корзины в шаблоне (Join данных)
type CartViewItem struct {
	SolarEquipment
	SolarRequestStr
}

// Структура для отображения элементов на главной странице каталога
type CatalogPageData struct {
	Equipments       []SolarEquipment // Список всех товаров для цикла
	CurrentRequestID int              // ID текущей активной заявки (0, если новой заявки еще нет)
}

// Глобальные переменные для БД и шаблонов
var (
	dbORM *gorm.DB
	dbSQL *sql.DB
	tmpl  *template.Template
)

func startAdminer() {
	log.Println("Запуск Adminer для PostgreSQL...")

	// Используем рабочее зеркало mirror.gcr.io и свободный порт 8090
	cmd := exec.Command("docker", "run", "-d", 
		"--rm",                              // Контейнер удалится после остановки приложения
		"--name", "solar_project_adminer",    // Имя, чтобы контейнеры не плодились
		"-p", "8090:8080", 
		"mirror.gcr.io/library/adminer:latest",
	)

	// Выполняем команду
	if err := cmd.Run(); err != nil {
		// Ошибка может быть, если контейнер с таким именем уже запущен
		log.Printf("Заметка по Adminer (возможно уже запущен): %v", err)
		log.Println("Adminer доступен на http://localhost:8090")
		return
	}

	// Небольшая пауза, чтобы контейнер успел подняться
	//time.Sleep(1 * time.Second)
	log.Println("Adminer доступен на http://localhost:8090")
}

func main() {
	
	// --- БЛОК АВТОМАТИЗАЦИИ POSTGRESQL ---
	// Запуск локального сервера PostgreSQL из бинарников
	pgCmd := exec.Command("C:\\PostgreSQL\\pgsql\\bin\\pg_ctl.exe", "-D", "C:\\PostgreSQL\\pgsql\\data", "-l", "logfile", "start")
	
	// Запускаем Adminer в отдельной горутине, чтобы он не блокировал main
	go startAdminer()

	// Выполняем команду запуска СУБД
	if pgErr := pgCmd.Run(); pgErr != nil {
		// Если сервер уже запущен, pg_ctl вернет ошибку, это нормально, просто логируем
		log.Printf("Заметка по PostgreSQL (возможно уже запущен): %v\n", pgErr)
	} else {
		log.Println("Локальный сервер PostgreSQL успешно запущен!")
	}

	// --- БЛОК АВТОМАТИЗАЦИИ MINIO ---
	containerName := "minio-go-launcher"
	dataPath := "./minio_storage"

	cmdArgs := []string{
		"-d", "Ubuntu", "docker", "run", "-d",
		"--name", containerName,
		"-p", "9000:9000",
		"-p", "9001:9001",
		"-v", dataPath + ":/data",
		"-e", "MINIO_ROOT_USER=admin",
		"-e", "MINIO_ROOT_PASSWORD=adminPass",
		"quay.io/minio/minio", "server", "/data", "--console-address", ":9001",
	}

	exec.Command("docker", "rm", "-f", containerName).Run()
	cmd := exec.Command("wsl", cmdArgs...)
	exec.Command("docker", "-it", containerName, "/bin/sh  mc alias set local http://localhost:9000 admin adminPass && mc anonymous set download local/solar").Run()

	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Ошибка при запуске MinIO: %v\nКонсоль: %s\n", err, string(output))
	} else {
		log.Println("Сервер MinIO успешно запущен на http://localhost:9001")
	}

	// --- БЛОК ИНИЦИАЛИЗАЦИИ БАЗЫ ДАННЫХ (Требование ЛР2) ---
	dsn := "host=localhost user=postgres password=secret dbname=solar_db port=5432 sslmode=disable client_encoding=UTF8"
	
	// 1. Подключение через ORM (GORM)
	dbORM, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("Ошибка подключения GORM: %v", err)
	}

	// Автомиграция структур в таблицы PostgreSQL
	dbORM.AutoMigrate(
		&SolarEquipment{}, 
		&SolarRequest{}, 
		&SolarRequestStr{}, 
		&SolarUsers{},
		&SolarInsolation{},
	)

	// Заполнение БД начальными данными, если пуста
	seedDatabaseIns(dbORM) 
	seedDatabase()

	// 2. Получаем чистый *sql.DB напрямую из готового dbORM)
	dbSQL, err = dbORM.DB()
	if err != nil {
		log.Fatalf("Ошибка получения sql.DB из GORM: %v", err)
	}

	// Парсинг шаблонов
	tmpl = template.Must(template.ParseGlob("templates/*.html"))

	// Раздача статики (CSS, JS)
	fs := http.FileServer(http.Dir("static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	// --- МАРШРУТИЗАЦИЯ (Регистрация обязательных 5 обработчиков ЛР2) ---
	// GET-запросы (Вывод страниц)
	http.HandleFunc("/", handleCatalog)          // 1. Поиск и вывод услуг (Твой "/" с поддержкой ORM)
	http.HandleFunc("/equipment/", handleDetail) // 2. Карточка одной услуги (Твой "/equipment/")
	http.HandleFunc("/request/", handleCart)     // 3. Просмотр корзины (Твой "/request/" с фильтрацией 'deleted')

	// POST-запросы (Модификация данных)
	http.HandleFunc("/request/add", handleRequestAdd)       // 4. Добавление в черновик (через ORM)
	http.HandleFunc("/request/delete", handleRequestDelete) // 5. Логическое удаление черновика (через сырой SQL UPDATE)

	log.Println("Сервер SolarCalc успешно запущен на http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// 1. Обработчик списка услуг (GET /) с поиском через GORM
func handleCatalog(w http.ResponseWriter, r *http.Request) {
	search := r.URL.Query().Get("search")
	var filtered []SolarEquipment

	query := dbORM
	if search != "" {
		query = query.Where("model_name ILIKE ?", "%"+search+"%")
	}
	if err := query.Find(&filtered).Error; err != nil {
		http.Error(w, "Ошибка получения каталога", http.StatusInternalServerError)
		return
	}

	// 1. Читаем ID заявки из куки (Cookie)
	reqID := 0
	if cookie, err := r.Cookie("request_id"); err == nil {
		reqID, _ = strconv.Atoi(cookie.Value)
	}

	// Считаем общее количество активных (не удаленных) позиций в корзине для CartCount
	// Фильтруем только по нашей текущей заявке reqID, чтобы не показывать чужие товары
	var totalCount int64
	dbORM.Model(&SolarRequestStr{}).Where("request_id = ? AND status != ?", reqID, "deleted").Count(&totalCount)

	// 2. Передаем CurrentRequestID в мапу для шаблона
	tmpl.ExecuteTemplate(w, "solar_index.html", map[string]interface{}{
		"Items":            filtered,
		"CartCount":        totalCount,
		"Search":           search,
		"CurrentRequestID": reqID, // Передаем на фронтенд
	})
}

// 2. Обработчик одной услуги (GET /equipment/{id}) через GORM
func handleDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/equipment/"))
	if err != nil {
		http.Error(w, "Некорректный ID оборудования", http.StatusBadRequest)
		return
	}

	var found SolarEquipment
	if err := dbORM.First(&found, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			http.Error(w, "Товар не найден", http.StatusNotFound)
		} else {
			http.Error(w, "Ошибка базы данных", http.StatusInternalServerError)
		}
		return
	}

	// 1. Читаем ID существующей заявки из куки браузера
	reqID := 0
	if cookie, err := r.Cookie("request_id"); err == nil {
		reqID, _ = strconv.Atoi(cookie.Value)
	}

	// 2. Передаем CurrentRequestID в шаблон "solar_item.html"
	tmpl.ExecuteTemplate(w, "solar_item.html", map[string]interface{}{
		"Equipment":        found,
		"CurrentRequestID": reqID, // Теперь ID заявки доступен в HTML
	})
}

// 3. Обработчик состава заявки (GET /request/{request_id}) с исключением статуса 'deleted'
func handleCart(w http.ResponseWriter, r *http.Request) {
	reqID, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/request/"))
	if err != nil {
		http.Error(w, "Некорректный ID заявки", http.StatusBadRequest)
		return
	}

	var activeRelations []SolarRequestStr
	// Требование ЛР2: Скрывать элементы со статусом 'deleted'
	if err := dbORM.Where("request_id = ? AND status != ?", reqID, "deleted").Find(&activeRelations).Error; err != nil {
		http.Error(w, "Ошибка получения данных корзины", http.StatusInternalServerError)
		return
	}

	// Собираем View-структуру для шаблона (имитация JOIN-а)
	var viewItems []CartViewItem
	for _, ri := range activeRelations {
		var eq SolarEquipment
		if err := dbORM.First(&eq, ri.EquipmentID).Error; err == nil {
			viewItems = append(viewItems, CartViewItem{
				SolarEquipment:  eq,
				SolarRequestStr: ri,
			})
		}
	}

	// Вычисляем общее количество активных элементов в корзине
	var totalCount int64
	dbORM.Model(&SolarRequestStr{}).Where("status != ?", "deleted").Count(&totalCount)

	tmpl.ExecuteTemplate(w, "solar_request.html", map[string]interface{}{
		"RequestID": reqID,
		"CartCount": totalCount,
		"CartItems": viewItems,
	})
}

// 4. POST /request/add — Добавление услуги в черновик через ORM (GORM)
func handleRequestAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	eqID, _ := strconv.Atoi(r.FormValue("equipment_id"))
	reqID, _ := strconv.Atoi(r.FormValue("request_id")) // может прийти 0, если это новая заявка
	qty, _ := strconv.Atoi(r.FormValue("quantity"))

	if qty <= 0 {
		qty = 1
	}

	// Проверка на реальное существование товара в БД
	var eq SolarEquipment
	if err := dbORM.First(&eq, eqID).Error; err != nil {
		http.Error(w, "Товар не существует", http.StatusBadRequest)
		return
	}

	// ЕСЛИ REQ_ID НЕ ПЕРЕДАН (равен 0), СОЗДАЕМ НОВУЮ ЗАЯВКУ В БД
	if reqID <= 0 {
		// Добавили явное заполнение всех полей по умолчанию для PostgreSQL
		queryNewReq := `
			INSERT INTO solar_requests (status, user_id, latitude, insolation, created_at) 
			VALUES ('draft', 2, 0.0, 0.0, NOW()) 
			RETURNING id`
		
		err := dbSQL.QueryRow(queryNewReq).Scan(&reqID)
		if err != nil {
			log.Printf("[КРИТИЧЕСКАЯ ОШИБКА] Не удалось сгенерировать новую заявку в БД: %v", err)
			http.Error(w, "Ошибка при создании новой заявки в БД", http.StatusInternalServerError)
			return
		}
			
		// ЗАПИСЫВАЕМ ID В КУКИ БРАУЗЕРА (на 30 дней)
		http.SetCookie(w, &http.Cookie{
			Name:     "request_id",
			Value:    strconv.Itoa(reqID),
			Path:     "/", // доступна на всем сайте
			MaxAge:   86400 * 30,
			HttpOnly: true, // защита от XSS скриптов
		})
		
		log.Printf("создана новая заявка: %d", reqID)
	}

	// Теперь у нас гарантированно есть валидный reqID (существующий или только что созданный)
	var existingRecord SolarRequestStr
	err := dbORM.Where("request_id = ? AND equipment_id = ? AND status = ?", reqID, eqID, "draft").First(&existingRecord).Error

	if err == nil {
		// Запись найдена -> ОБНОВЛЯЕМ количество
		existingRecord.Quantity += qty
		if err := dbORM.Save(&existingRecord).Error; err != nil {
			http.Error(w, "Ошибка обновления количества в базе данных", http.StatusInternalServerError)
			return
		}
	} else if errors.Is(err, gorm.ErrRecordNotFound) {
		// Запись не найдена -> СОЗДАЕМ новую строку через сырой SQL,
		// так как GORM не поддерживает частичные индексы в ON CONFLICT из коробки.
		rawSQL := `
			INSERT INTO "solar_request_strs" ("request_id", "equipment_id", "status", "quantity") 
			VALUES ($1, $2, 'draft', $3) 
			ON CONFLICT ("request_id", "equipment_id", "status") WHERE status != 'deleted' 
			DO UPDATE SET "quantity" = solar_request_strs.quantity + EXCLUDED.quantity`
		
		err := dbORM.Exec(rawSQL, reqID, eqID, qty).Error
		if err != nil {
			log.Printf("Ошибка Raw SQL INSERT в handleRequestAdd: %v", err)
			http.Error(w, "Ошибка сохранения в базу данных", http.StatusInternalServerError)
			return
		}
	} else {
		http.Error(w, "Ошибка проверки существования записи", http.StatusInternalServerError)
		return
	}
	
	// Перенаправление на страницу новой или обновленной корзины
	http.Redirect(w, r, fmt.Sprintf("/request/%d", reqID), http.StatusSeeOther)
}

// 5. POST /request/delete — Логическое удаление строки черновика через сырой SQL UPDATE
func handleRequestDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	idStr := r.FormValue("id")
	reqIDStr := r.FormValue("request_id")

	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Некорректный ID записи", http.StatusBadRequest)
		return
	}

	reqID, err := strconv.Atoi(reqIDStr)
	if err != nil {
		http.Error(w, "Некорректный ID заявки", http.StatusBadRequest)
		return
	}

	// 1. Логическое удаление конкретной строки товара
	query := "UPDATE solar_request_strs SET status = 'deleted' WHERE id = $1"
	_, err = dbSQL.Exec(query, id)
	if err != nil {
		log.Printf("Ошибка при UPDATE id=%d в solar_request_strs: %v", id, err) 
		http.Error(w, "Ошибка при выполнении удаления из БД", http.StatusInternalServerError)
		return
	}

	// 2. Проверяем, остались ли ЕЩЕ активные строки ('draft') у этой заявки (reqID)
	var exists bool
	queryCheck := "SELECT EXISTS(SELECT 1 FROM solar_request_strs WHERE status = 'draft' AND request_id = $1)"
	
	err = dbSQL.QueryRow(queryCheck, reqID).Scan(&exists)
	if err != nil {
		log.Printf("Ошибка проверки существования строк для request_id=%d: %v", reqID, err)
		http.Error(w, "Ошибка проверки структуры корзины", http.StatusInternalServerError)
		return
	}

	// 3. Если активных строк больше нет, удаляем саму заявку (логически)
	if !exists {
		queryDeleteRequest := "UPDATE solar_requests SET status = 'deleted' WHERE id = $1"
		_, err = dbSQL.Exec(queryDeleteRequest, reqID)
		if err != nil {
			log.Printf("Ошибка логического удаления заявки request_id=%d: %v", reqID, err)
			http.Error(w, "Ошибка при обновлении статуса заявки", http.StatusInternalServerError)
			return
		}

		// КРИТИЧЕСКИ ВАЖНО: Удаляем куку из браузера, чтобы следующая заявка создавалась с НОВЫМ ID
		http.SetCookie(w, &http.Cookie{
			Name:   "request_id",
			Value:  "",
			Path:   "/",
			MaxAge: -1, // Передача отрицательного MaxAge мгновенно удаляет куку в браузере
		})

		// Так как заявка теперь удалена, её страницы больше нет — отправляем на главную
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	
	// 4. Если в заявке еще остались другие товары, возвращаем пользователя в корзину
	http.Redirect(w, r, "/request/"+reqIDStr, http.StatusSeeOther)
}

// 6. POST /request/delete_request — Логическое удаление всего черновика через сырой SQL UPDATE
func handleRequestDeleteRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	// Получаем ID записи заявки
	reqID := r.FormValue("id")
	
	id, err := strconv.Atoi(reqID)
	if err != nil {
		http.Error(w, "Некорректный ID записи", http.StatusBadRequest)
		return
	}

	// Требование ЛР2: Выполнение сырого SQL UPDATE через *sql.DB
	query := "UPDATE solar_requests SET status = 'deleted' WHERE id = $1"
	_, err = dbSQL.Exec(query, id)
	if err != nil {
		http.Error(w, "Ошибка при выполнении удаления заявки из БД", http.StatusInternalServerError)
		return
	}

	// Каскадно помечаем удаленными все позиции оборудования в этой заявке
	queryItems := "UPDATE solar_request_strs SET status = 'deleted' WHERE request_id = $1"
	_, err = dbSQL.Exec(queryItems, id)
	if err != nil {
		http.Error(w, "Ошибка при выполнении удаления строк из БД", http.StatusInternalServerError)
		return
	}
	
	// Возвращаем пользователя на главную
	http.Redirect(w, r, "/", http.StatusSeeOther)
	
}

// 7. Функция расчета выработки панелей по формуле в зависимости от широты местности 
// CalculateSolarGenerationDB рассчитывает годовую выработку, используя данные из БД
// latitude - широта местности (в градусах, от -80 до 80)
// power - номинальная мощность солнечных панелей (кВт)
// efficiency - коэффициент потерь системы (k, обычно 0.7 - 0.85, для модели берем 0.7)
func CalculateSolarGenerationDB(db *gorm.DB, latitude float64, power float64, efficiency float64) (float64, error) {
	absLat := math.Abs(latitude)

	if absLat > 80.0 {
		return 0, fmt.Errorf("широта %.2f выходит за пределы таблицы (макс 80 градусов)", latitude)
	}
	if power <= 0 || efficiency <= 0 || efficiency > 1 {
		return 0, fmt.Errorf("некорректные параметры мощности или КПД")
	}

	var lowerPoint, upperPoint SolarInsolation

	// Ищем ближайшую точку снизу (или точное совпадение)
	err := db.Where("latitude <= ?", absLat).Order("latitude DESC").First(&lowerPoint).Error
	if err != nil {
		return 0, fmt.Errorf("не удалось найти нижнюю границу широты: %w", err)
	}

	// Если широта совпала идеально, интерполяция не нужна
	if lowerPoint.Latitude == absLat {
		generation := power * lowerPoint.KwhM2 * efficiency
		return generation, nil
	}

	// Ищем ближайшую точку сверху
	err = db.Where("latitude > ?", absLat).Order("latitude ASC").First(&upperPoint).Error
	if err != nil {
		return 0, fmt.Errorf("не удалось найти верхнюю границу широты: %w", err)
	}

	// Линейная интерполяция по точкам из БД
	x0, y0 := lowerPoint.Latitude, lowerPoint.KwhM2
	x1, y1 := upperPoint.Latitude, upperPoint.KwhM2
	
	annualInsolation := y0 + (absLat-x0)*(y1-y0)/(x1-x0)

	// Формула: E = P * Hi * k
	generation := power * annualInsolation * efficiency
	return generation, nil
}

// Функции первичного наполнения СУБД (Seed)
// seedDatabaseIns выполняет первоначальное заполнение справочников
func seedDatabaseIns(dbORM *gorm.DB) {
	var count int64
	
	// Проверяем таблицу инсоляции
	err := dbORM.Model(&SolarInsolation{}).Count(&count).Error
	if err != nil {
		fmt.Printf("Ошибка при подсчете записей инсоляции: %v\n", err)
	}

	if count == 0 {
		initialInsolation := []SolarInsolation{
			{Latitude: 0, KwhM2: 2044},
			{Latitude: 5, KwhM2: 2007},
			{Latitude: 10, KwhM2: 2007},
			{Latitude: 15, KwhM2: 2007},
			{Latitude: 20, KwhM2: 2044},
			{Latitude: 25, KwhM2: 2080},
			{Latitude: 30, KwhM2: 2007},
			{Latitude: 35, KwhM2: 1898},
			{Latitude: 40, KwhM2: 1752},
			{Latitude: 45, KwhM2: 1606},
			{Latitude: 50, KwhM2: 1460},
			{Latitude: 55, KwhM2: 1314},
			{Latitude: 60, KwhM2: 1168},
			{Latitude: 65, KwhM2: 1022},
			{Latitude: 70, KwhM2: 876},
			{Latitude: 75, KwhM2: 766},
			{Latitude: 80, KwhM2: 657},
		}
		
		// Транзакционная вставка данных
		dbORM.Create(&initialInsolation)
	}
}

func seedDatabase() {
	var count int64
	dbORM.Model(&SolarEquipment{}).Count(&count)
	if count == 0 {
		initialEquipments := []SolarEquipment{
			{ModelName: "Аккумулятор Vektor GL 12-100", Price: 10000.0, Type: "capacity", Power: 0, Capacity: 100, Description: "Аккумуляторные батареи VEKTOR ENERGY серии GEL (GL) изготовлены по технологии AGM+GEL. Электролит увязан в гель посредством оксида кремния SiO2. Имеют отличные разрядные и эксплуатационные характеристики.", ImageKey: "AKB100.jpg", VideoKey: "solar.webm", Status: "active"},
			{ModelName: "Аккумулятор Vektor GL 12-150", Price: 15000.0, Type: "capacity", Power: 0, Capacity: 150, Description: "Аккумуляторные батареи VEKTOR ENERGY серии GEL (GL) изготовлены по технологии AGM+GEL. Электролит увязан в гель посредством оксида кремния SiO2. Имеют отличные разрядные и эксплуатационные характеристики.", ImageKey: "AKB150.jpg", VideoKey: "solar.webm", Status: "active"},
			{ModelName: "Аккумулятор Vektor GL 12-200", Price: 20000.0, Type: "capacity", Power: 0, Capacity: 200, Description: "Аккумуляторные батареи VEKTOR ENERGY серии GEL (GL) изготовлены по технологии AGM+GEL. Электролит увязан в гель посредством оксида кремния SiO2. Имеют отличные разрядные и эксплуатационные характеристики.", ImageKey: "AKB200.jpg", VideoKey: "solar.webm", Status: "active"},
			{ModelName: "Солнечная панель DELTA_NXT500", Price: 12000.0, Type: "power", Power: 500, Capacity: 0, Description: "Фотоэлектрический солнечный модуль (ФСМ) DELTA NXT 400-54/2 M10 HC DELTA NXT - это серия фотоэлектрических модулей, выполненных из материалов экстра-класса. При невысокой интенсивности солнечного излучения, DELTA NXT вырабатывают больше электроэнергии, чем стандартные солнечные модули с аналогичными характеристиками.", ImageKey: "DELTA_NXT500.jpg", VideoKey: "solar1.webm", Status: "active"},
			{ModelName: "Солнечная панель GWS280", Price: 89000.0, Type: "power", Power: 200, Capacity: 0, Description: "При производстве солнечных панелей GWS используются высококачественные материалы, что гарантирует наивысшее качество изделий: прочный защитный слой специального закалённого стекла и усиленная рамка из анодированного алюминия, устойчивая к коррозии, обеспечивает высокий класс защиты от механических повреждений, влаги и высокое сопротивление экстремальной ветровой нагрузке.", ImageKey: "GWS280.jpg", VideoKey: "solar1.webm", Status: "active"},
			{ModelName: "Солнечная панель M300WT", Price: 26000.0, Type: "power", Power: 300, Capacity: 0, Description: "Солнечные батареи серии М300 являются фотоэлектрическими модулями, выполненными из материалов экстра-класса. При невысокой интенсивности солнечного излучения, вырабатывают больше электроэнергии, чем стандартные солнечные модули с аналогичными характеристиками.", ImageKey: "M300WT.jpg", VideoKey: "solar1.webm", Status: "active"},
		}
		dbORM.Create(&initialEquipments)
	}

	dbORM.Model(&SolarRequest{}).Count(&count)
	if count == 0 {
		initialRequest := []SolarRequest{
			{UserID: 2, CreatedAt: time.Now(), Latitude: 45,	Insolation: 4.3, Status: "draft"},
		}
		dbORM.Create(&initialRequest)
	}

	dbORM.Model(&SolarRequestStr{}).Count(&count)
	if count == 0 {
		initialRequestStr := []SolarRequestStr{
			{RequestID: 1, EquipmentID: 4, Quantity: 4, Status: "draft"},
			{RequestID: 1, EquipmentID: 2, Quantity: 1, Status: "draft"},
			{RequestID: 1, EquipmentID: 6, Quantity: 2, Status: "draft"},
		}
		dbORM.Create(&initialRequestStr)
	}

	dbORM.Model(&SolarUsers{}).Count(&count)
	if count == 0 {
		initialUsers := []SolarUsers{
			{Name: "Гость", Role: "guest", Status: "active"},
			{Name: "Клиент", Role: "client", Status: "active"},
			{Name: "Модератор", Role: "moderator", Status: "active"},
			{Name: "Сервис", Role: "service", Status: "active"},
		}
		dbORM.Create(&initialUsers)
	}
}
