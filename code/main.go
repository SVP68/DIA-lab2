package main

import (
	"database/sql"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os/exec"
	"strconv"
	"strings"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// SolarEquipment - Первая модель-коллекция (Услуги)
type SolarEquipment struct {
	ID            int     `gorm:"primaryKey;column:id"`
	ModelName     string  `gorm:"column:model_name"`
	Price         float64 `gorm:"column:price"`
	CapacityPower int     `gorm:"column:capacity_power"`
	Description   string  `gorm:"column:description"`
	ImageKey      string  `gorm:"column:image_key"`
	VideoKey      string  `gorm:"column:video_key"`
}

func (SolarEquipment) TableName() string {
	return "solar_equipments"
}

// CapacityPowerStr - Вторая модель-коллекция (Состав заявки)
type CapacityPowerStr struct {
	ID          int    `gorm:"primaryKey;column:id"`          // ID записи в БД для точечного UPDATE
	RequestID   int    `gorm:"column:request_id"`             // Связующий ID заявки
	EquipmentID int    `gorm:"column:equipment_id"`           // ID товара
	Quantity    int    `gorm:"column:quantity"`               // Количество
	Status      string `gorm:"column:status;default:'draft'"` // "draft" или "deleted" (Требование ЛР2)
}

func (CapacityPowerStr) TableName() string {
	return "capacity_power_strs"
}

// Структура для отображения элементов корзины в шаблоне (Join данных)
type CartViewItem struct {
	SolarEquipment
	CapacityPowerStr
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
	dbORM.AutoMigrate(&SolarEquipment{}, &CapacityPowerStr{})
	seedDatabase() // Заполнение БД начальными данными, если пуста

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
		// Фильтрация без учёта регистра по подстроке средствами БД (ILIKE)
		query = query.Where("model_name ILIKE ?", "%"+search+"%")
	}
	if err := query.Find(&filtered).Error; err != nil {
		http.Error(w, "Ошибка получения каталога", http.StatusInternalServerError)
		return
	}

	// Считаем общее количество активных (не удаленных) позиций в корзине для CartCount
	var totalCount int64
	dbORM.Model(&CapacityPowerStr{}).Where("status != ?", "deleted").Count(&totalCount)

	tmpl.ExecuteTemplate(w, "solar_index.html", map[string]interface{}{
		"Items":     filtered,
		"CartCount": totalCount,
		"Search":    search,
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

	tmpl.ExecuteTemplate(w, "solar_item.html", map[string]interface{}{
		"Equipment": found,
	})
}

// 3. Обработчик состава заявки (GET /request/{request_id}) с исключением статуса 'deleted'
func handleCart(w http.ResponseWriter, r *http.Request) {
	reqID, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/request/"))
	if err != nil {
		http.Error(w, "Некорректный ID заявки", http.StatusBadRequest)
		return
	}

	var activeRelations []CapacityPowerStr
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
				SolarEquipment:   eq,
				CapacityPowerStr: ri,
			})
		}
	}

	// Вычисляем общее количество активных элементов в корзине
	var totalCount int64
	dbORM.Model(&CapacityPowerStr{}).Where("status != ?", "deleted").Count(&totalCount)

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
	reqID, _ := strconv.Atoi(r.FormValue("request_id"))
	qty, _ := strconv.Atoi(r.FormValue("quantity"))

	if qty <= 0 {
		qty = 1
	}
	if reqID <= 0 {
		reqID = 101 // ID по умолчанию, если не передан
	}

	// Проверка на реальное существование услуги в БД
	var eq SolarEquipment
	if err := dbORM.First(&eq, eqID).Error; err != nil {
		http.Error(w, "Товар не существует", http.StatusBadRequest)
		return
	}

	// Новая запись в черновик со статусом 'draft'
	newRecord := CapacityPowerStr{
		RequestID:   reqID,
		EquipmentID: eqID,
		Quantity:    qty,
		Status:      "draft",
	}

	if err := dbORM.Create(&newRecord).Error; err != nil {
		http.Error(w, "Ошибка добавления в базу данных", http.StatusInternalServerError)
		return
	}

	// Перенаправление на страницу этой корзины
	http.Redirect(w, r, fmt.Sprintf("/request/%d", reqID), http.StatusSeeOther)
}

// 5. POST /request/delete — Логическое удаление черновика через сырой SQL UPDATE
func handleRequestDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	// Получаем ID записи таблицы capacity_power_strs и ID корзины для возврата
	idStr := r.FormValue("id")
	reqIDStr := r.FormValue("request_id")
	
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Некорректный ID записи", http.StatusBadRequest)
		return
	}

	// Требование ЛР2: Выполнение сырого SQL UPDATE через *sql.DB
	query := "UPDATE capacity_power_strs SET status = 'deleted' WHERE id = $1"
	_, err = dbSQL.Exec(query, id)
	if err != nil {
		http.Error(w, "Ошибка при выполнении удаления из БД", http.StatusInternalServerError)
		return
	}

	// Возвращаем пользователя обратно в корзину
	http.Redirect(w, r, "/request/"+reqIDStr, http.StatusSeeOther)
}

// Функция первичного наполнения СУБД (Seed)
func seedDatabase() {
	var count int64
	dbORM.Model(&SolarEquipment{}).Count(&count)
	if count == 0 {
		initialEquipments := []SolarEquipment{
			{ID: 1,	ModelName: "Аккумулятор Vektor GL 12-100", Price: 10000.0, CapacityPower: 100, Description: "Аккумуляторные батареи VEKTOR ENERGY серии GEL (GL) изготовлены по технологии AGM+GEL. Электролит увязан в гель посредством оксида кремния SiO2. Имеют отличные разрядные и эксплуатационные характеристики.", ImageKey: "AKB100.jpg", VideoKey: "solar.webm",},
			{ID: 2, ModelName: "Аккумулятор Vektor GL 12-150", Price: 15000.0, CapacityPower: 150, Description: "Аккумуляторные батареи VEKTOR ENERGY серии GEL (GL) изготовлены по технологии AGM+GEL. Электролит увязан в гель посредством оксида кремния SiO2. Имеют отличные разрядные и эксплуатационные характеристики.", ImageKey: "AKB150.jpg", VideoKey: "solar.webm"},
			{ID: 3, ModelName: "Аккумулятор Vektor GL 12-200", Price: 20000.0, CapacityPower: 200, Description: "Аккумуляторные батареи VEKTOR ENERGY серии GEL (GL) изготовлены по технологии AGM+GEL. Электролит увязан в гель посредством оксида кремния SiO2. Имеют отличные разрядные и эксплуатационные характеристики.", ImageKey: "AKB200.jpg", VideoKey: "solar.webm"},
			{ID: 4, ModelName: "Солнечная панель DELTA_NXT500", Price: 12000.0, CapacityPower: 500, Description: "Фотоэлектрический солнечный модуль (ФСМ) DELTA NXT 400-54/2 M10 HC DELTA NXT - это серия фотоэлектрических модулей, выполненных из материалов экстра-класса. При невысокой интенсивности солнечного излучения, DELTA NXT вырабатывают больше электроэнергии, чем стандартные солнечные модули с аналогичными характеристиками.", ImageKey: "DELTA_NXT500.jpg", VideoKey: "solar1.webm"},
			{ID: 5, ModelName: "Солнечная панель GWS280", Price: 89000.0, CapacityPower: 280, Description: "При производстве солнечных панелей GWS используются высококачественные материалы, что гарантирует наивысшее качество изделий: прочный защитный слой специального закалённого стекла и усиленная рамка из анодированного алюминия, устойчивая к коррозии, обеспечивает высокий класс защиты от механических повреждений, влаги и высокое сопротивление экстремальной ветровой нагрузке.", ImageKey: "GWS280.jpg", VideoKey: "solar1.webm"},
			{ID: 6, ModelName: "Солнечная панель M300WT", Price: 26000.0, CapacityPower: 300, Description: "Солнечные батареи серии М300 являются фотоэлектрическими модулями, выполненными из материалов экстра-класса. При невысокой интенсивности солнечного излучения, вырабатывают больше электроэнергии, чем стандартные солнечные модули с аналогичными характеристиками.", ImageKey: "M300WT.jpg", VideoKey: "solar1.webm"},
		}
		dbORM.Create(&initialEquipments)
	}

	dbORM.Model(&CapacityPowerStr{}).Count(&count)
	if count == 0 {
		initialCapacity := []CapacityPowerStr{
			{RequestID: 101, EquipmentID: 4, Quantity: 4, Status: "draft"},
			{RequestID: 101, EquipmentID: 2, Quantity: 1, Status: "draft"},
			{RequestID: 101, EquipmentID: 6, Quantity: 2, Status: "draft"},
		}
		dbORM.Create(&initialCapacity)
	}
}