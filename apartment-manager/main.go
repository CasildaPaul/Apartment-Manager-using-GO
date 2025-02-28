package main

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	_ "github.com/mattn/go-sqlite3"
	"github.com/xuri/excelize/v2"
)

type Apartment struct {
	ID       string
	Owner    string
	Resident string
	SameFlag bool
}

func main() {
	// Initialize the app
	a := app.New()
	w := a.NewWindow("Apartment Manager")
	w.Resize(fyne.NewSize(800, 600))

	// Initialize database
	db, err := initDB()
	if err != nil {
		dialog.ShowError(err, w)
		return
	}
	defer db.Close()

	// Current apartment data
	var currentApartment Apartment

	// Create input fields
	idEntry := widget.NewEntry()
	idEntry.SetPlaceHolder("Apartment ID")

	ownerEntry := widget.NewEntry()
	ownerEntry.SetPlaceHolder("Owner")

	residentEntry := widget.NewEntry()
	residentEntry.SetPlaceHolder("Resident")

	// Checkbox for same owner/resident
	sameCheck := widget.NewCheck("Owner is Resident", nil)

	// Set up change callbacks after all UI elements are created
	ownerEntry.OnChanged = func(value string) {
		currentApartment.Owner = value
		updateSameFlag(&currentApartment)
		if currentApartment.SameFlag {
			sameCheck.SetChecked(true)
		}
	}

	residentEntry.OnChanged = func(value string) {
		if value == "" {
			currentApartment.Resident = "Vacant"
			residentEntry.SetText("Vacant")
		} else {
			currentApartment.Resident = value
		}
		updateSameFlag(&currentApartment)
		if currentApartment.SameFlag {
			sameCheck.SetChecked(true)
		}
	}

	// sameCheck.OnChanged = func(value bool) {
	// 	currentApartment.SameFlag = value
	// 	if value {
	// 		residentEntry.SetText(ownerEntry.Text)
	// 		currentApartment.Resident = currentApartment.Owner
	// 	}
	// }

	// Create a list to display apartments
	apartmentsList := widget.NewList(
		func() int {
			return getApartmentCount(db)
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("Template")
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			apt := getApartmentByIndex(db, id)
			label := obj.(*widget.Label)
			label.SetText(fmt.Sprintf("ID: %s | Owner: %s | Resident: %s", apt.ID, apt.Owner, apt.Resident))
		},
	)

	// Refresh function for the list
	refreshList := func() {
		apartmentsList.Refresh()
	}

	// Button to add/update apartment
	saveBtn := widget.NewButtonWithIcon("Save", theme.DocumentSaveIcon(), func() {
		currentApartment.ID = idEntry.Text
		currentApartment.Owner = ownerEntry.Text
		if residentEntry.Text == "" {
			currentApartment.Resident = "Vacant"
		} else {
			currentApartment.Resident = residentEntry.Text
		}

		if currentApartment.ID == "" {
			dialog.ShowError(fmt.Errorf("apartment ID cannot be empty"), w)
			return
		}

		err := saveApartment(db, currentApartment)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}

		// Clear fields
		idEntry.SetText("")
		ownerEntry.SetText("")
		residentEntry.SetText("")
		sameCheck.SetChecked(false)

		// Refresh the list
		refreshList()
	})

	// Button to verify owner/resident
	// verifyBtn := widget.NewButtonWithIcon("Verify Owner=Resident", theme.ConfirmIcon(), func() {
	// 	updateSameFlag(&currentApartment)
	// 	if currentApartment.SameFlag {
	// 		sameCheck.SetChecked(true)
	// 		dialog.ShowInformation("Verification", "Owner is the Resident", w)
	// 	} else {
	// 		sameCheck.SetChecked(false)
	// 		dialog.ShowInformation("Verification", "Owner is not the Resident", w)
	// 	}
	// })

	// Import from CSV/Excel button
	importBtn := widget.NewButtonWithIcon("Import Data", theme.FolderOpenIcon(), func() {
		fd := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil || reader == nil {
				return
			}
			defer reader.Close()

			filePath := reader.URI().Path()
			ext := strings.ToLower(filepath.Ext(filePath))

			var importErr error
			if ext == ".csv" {
				importErr = importFromCSV(db, filePath, refreshList)
			} else if ext == ".xlsx" {
				importErr = importFromExcel(db, filePath, refreshList)
			} else {
				dialog.ShowError(fmt.Errorf("unsupported file format: %s", ext), w)
				return
			}

			if importErr != nil {
				dialog.ShowError(importErr, w)
				return
			}

			dialog.ShowInformation("Import Successful", "Data has been imported successfully", w)
			refreshList()
		}, w)
		fd.SetFilter(storage.NewExtensionFileFilter([]string{".csv", ".xlsx"}))
		fd.Show()
	})

	// Export to CSV/Excel button
	exportBtn := widget.NewButtonWithIcon("Export Data", theme.DownloadIcon(), func() {
		fd := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
			if err != nil || writer == nil {
				return
			}
			defer writer.Close()

			filePath := writer.URI().Path()
			ext := strings.ToLower(filepath.Ext(filePath))

			var exportErr error
			if ext == ".csv" {
				exportErr = exportToCSV(db, filePath)
			} else if ext == ".xlsx" {
				exportErr = exportToExcel(db, filePath)
			} else {
				dialog.ShowError(fmt.Errorf("unsupported export format: %s", ext), w)
				return
			}

			if exportErr != nil {
				dialog.ShowError(exportErr, w)
				return
			}

			dialog.ShowInformation("Export Successful", "Data has been exported successfully", w)
		}, w)
		fd.SetFilter(storage.NewExtensionFileFilter([]string{".csv", ".xlsx"}))
		fd.Show()
	})

	// List selection handler
	apartmentsList.OnSelected = func(id widget.ListItemID) {
		apt := getApartmentByIndex(db, id)
		currentApartment = apt

		idEntry.SetText(apt.ID)
		ownerEntry.SetText(apt.Owner)
		residentEntry.SetText(apt.Resident)
		sameCheck.SetChecked(apt.SameFlag)
	}

	// Create delete button
	deleteBtn := widget.NewButtonWithIcon("Delete", theme.DeleteIcon(), func() {
		if idEntry.Text == "" {
			dialog.ShowError(fmt.Errorf("please select an apartment to delete"), w)
			return
		}

		dialog.ShowConfirm("Confirm Deletion",
			fmt.Sprintf("Are you sure you want to delete Apartment %s?", idEntry.Text),
			func(confirmed bool) {
				if confirmed {
					err := deleteApartment(db, idEntry.Text)
					if err != nil {
						dialog.ShowError(err, w)
						return
					}

					// Clear fields
					idEntry.SetText("")
					ownerEntry.SetText("")
					residentEntry.SetText("")
					sameCheck.SetChecked(false)

					// Refresh the list
					refreshList()
				}
			}, w)
	})

	// Create clear button
	clearBtn := widget.NewButtonWithIcon("Clear Form", theme.CancelIcon(), func() {
		idEntry.SetText("")
		ownerEntry.SetText("")
		residentEntry.SetText("")
		sameCheck.SetChecked(false)
		apartmentsList.UnselectAll()
	})

	// Create layout
	inputForm := container.NewVBox(
		widget.NewLabel("Apartment Details"),
		idEntry,
		ownerEntry,
		residentEntry,
		sameCheck,
		container.NewHBox(
			saveBtn,
			deleteBtn,
			clearBtn,
			// verifyBtn,
		),
		container.NewHBox(
			importBtn,
			exportBtn,
		),
	)

	// Create a split layout
	split := container.NewHSplit(
		container.NewBorder(nil, nil, nil, nil, apartmentsList),
		container.NewBorder(nil, nil, nil, nil, container.NewPadded(inputForm)),
	)
	split.SetOffset(0.4)

	// Set the content of the window
	w.SetContent(split)
	w.ShowAndRun()
}

// Initialize the database
func initDB() (*sql.DB, error) {
	db, err := sql.Open("sqlite3", "./apartments.db")
	if err != nil {
		return nil, err
	}

	// Create table if it doesn't exist
	statement, err := db.Prepare(`
		CREATE TABLE IF NOT EXISTS apartments (
			id TEXT PRIMARY KEY,
			owner TEXT NOT NULL,
			resident TEXT NOT NULL,
			same_flag INTEGER NOT NULL
		)
	`)
	if err != nil {
		return nil, err
	}

	_, err = statement.Exec()
	if err != nil {
		return nil, err
	}

	return db, nil
}

// Update the same flag based on owner and resident
func updateSameFlag(apt *Apartment) {
	apt.SameFlag = apt.Owner != "" && apt.Owner == apt.Resident
}

// Save apartment to database
func saveApartment(db *sql.DB, apt Apartment) error {
	// Check if apartment exists
	var exists bool
	err := db.QueryRow("SELECT 1 FROM apartments WHERE id = ?", apt.ID).Scan(&exists)

	if err != nil && err != sql.ErrNoRows {
		return err
	}

	if apt.Resident == "" {
		apt.Resident = "Vacant"
	}

	updateSameFlag(&apt)

	// Insert or update
	if err == sql.ErrNoRows {
		// Insert new record
		_, err = db.Exec(
			"INSERT INTO apartments (id, owner, resident, same_flag) VALUES (?, ?, ?, ?)",
			apt.ID, apt.Owner, apt.Resident, boolToInt(apt.SameFlag),
		)
	} else {
		// Update existing record
		_, err = db.Exec(
			"UPDATE apartments SET owner = ?, resident = ?, same_flag = ? WHERE id = ?",
			apt.Owner, apt.Resident, boolToInt(apt.SameFlag), apt.ID,
		)
	}

	return err
}

// Delete apartment from database
func deleteApartment(db *sql.DB, id string) error {
	_, err := db.Exec("DELETE FROM apartments WHERE id = ?", id)
	return err
}

// Get count of apartments
func getApartmentCount(db *sql.DB) int {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM apartments").Scan(&count)
	if err != nil {
		return 0
	}
	return count
}

// Get apartment by index
func getApartmentByIndex(db *sql.DB, index int) Apartment {
	var apt Apartment

	rows, err := db.Query("SELECT id, owner, resident, same_flag FROM apartments ORDER BY id LIMIT 1 OFFSET ?", index)
	if err != nil {
		return apt
	}
	defer rows.Close()

	if rows.Next() {
		var sameFlag int
		err = rows.Scan(&apt.ID, &apt.Owner, &apt.Resident, &sameFlag)
		if err != nil {
			return Apartment{}
		}
		apt.SameFlag = intToBool(sameFlag)
	}

	return apt
}

// Convert bool to int (SQLite doesn't have boolean type)
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Convert int to bool
func intToBool(i int) bool {
	return i == 1
}

// Import data from CSV file
func importFromCSV(db *sql.DB, filePath string, refreshFunc func()) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	reader := csv.NewReader(file)

	// Skip header
	_, err = reader.Read()
	if err != nil {
		return err
	}

	// Begin transaction
	tx, err := db.Begin()
	if err != nil {
		return err
	}

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			tx.Rollback()
			return err
		}

		if len(record) < 3 {
			continue
		}

		apt := Apartment{
			ID:       record[0],
			Owner:    record[1],
			Resident: record[2],
		}

		// Handle empty resident
		if apt.Resident == "" {
			apt.Resident = "Vacant"
		}

		updateSameFlag(&apt)

		// Check if apartment exists
		var exists bool
		err = tx.QueryRow("SELECT 1 FROM apartments WHERE id = ?", apt.ID).Scan(&exists)

		if err != nil && err != sql.ErrNoRows {
			tx.Rollback()
			return err
		}

		// Insert or update
		if err == sql.ErrNoRows {
			_, err = tx.Exec(
				"INSERT INTO apartments (id, owner, resident, same_flag) VALUES (?, ?, ?, ?)",
				apt.ID, apt.Owner, apt.Resident, boolToInt(apt.SameFlag),
			)
		} else {
			_, err = tx.Exec(
				"UPDATE apartments SET owner = ?, resident = ?, same_flag = ? WHERE id = ?",
				apt.Owner, apt.Resident, boolToInt(apt.SameFlag), apt.ID,
			)
		}

		if err != nil {
			tx.Rollback()
			return err
		}
	}

	return tx.Commit()
}

// Export data to CSV file
func exportToCSV(db *sql.DB, filePath string) error {
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header
	err = writer.Write([]string{"Apartment ID", "Owner", "Resident", "Owner is Resident"})
	if err != nil {
		return err
	}

	// Query all apartments
	rows, err := db.Query("SELECT id, owner, resident, same_flag FROM apartments ORDER BY id")
	if err != nil {
		return err
	}
	defer rows.Close()

	// Write data
	for rows.Next() {
		var apt Apartment
		var sameFlag int

		err = rows.Scan(&apt.ID, &apt.Owner, &apt.Resident, &sameFlag)
		if err != nil {
			return err
		}

		sameStatus := "No"
		if sameFlag == 1 {
			sameStatus = "Yes"
		}

		err = writer.Write([]string{apt.ID, apt.Owner, apt.Resident, sameStatus})
		if err != nil {
			return err
		}
	}

	return nil
}

// Import data from Excel file
func importFromExcel(db *sql.DB, filePath string, refreshFunc func()) error {
	f, err := excelize.OpenFile(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	// Get first sheet
	sheetName := f.GetSheetName(0)

	// Get all rows
	rows, err := f.GetRows(sheetName)
	if err != nil {
		return err
	}

	if len(rows) < 2 { // Need at least header and one data row
		return fmt.Errorf("not enough rows in Excel file")
	}

	// Begin transaction
	tx, err := db.Begin()
	if err != nil {
		return err
	}

	// Process data rows (skip header)
	for i := 1; i < len(rows); i++ {
		row := rows[i]
		if len(row) < 3 {
			continue
		}

		apt := Apartment{
			ID:       row[0],
			Owner:    row[1],
			Resident: row[2],
		}

		// Handle empty resident
		if apt.Resident == "" {
			apt.Resident = "Vacant"
		}

		updateSameFlag(&apt)

		// Check if apartment exists
		var exists bool
		err = tx.QueryRow("SELECT 1 FROM apartments WHERE id = ?", apt.ID).Scan(&exists)

		if err != nil && err != sql.ErrNoRows {
			tx.Rollback()
			return err
		}

		// Insert or update
		if err == sql.ErrNoRows {
			_, err = tx.Exec(
				"INSERT INTO apartments (id, owner, resident, same_flag) VALUES (?, ?, ?, ?)",
				apt.ID, apt.Owner, apt.Resident, boolToInt(apt.SameFlag),
			)
		} else {
			_, err = tx.Exec(
				"UPDATE apartments SET owner = ?, resident = ?, same_flag = ? WHERE id = ?",
				apt.Owner, apt.Resident, boolToInt(apt.SameFlag), apt.ID,
			)
		}

		if err != nil {
			tx.Rollback()
			return err
		}
	}

	return tx.Commit()
}

// Export data to Excel file
func exportToExcel(db *sql.DB, filePath string) error {
	f := excelize.NewFile()

	// Set headers
	f.SetCellValue("Sheet1", "A1", "Apartment ID")
	f.SetCellValue("Sheet1", "B1", "Owner")
	f.SetCellValue("Sheet1", "C1", "Resident")
	f.SetCellValue("Sheet1", "D1", "Owner is Resident")

	// Style the header
	style, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Bold: true,
		},
		Fill: excelize.Fill{
			Type:    "pattern",
			Color:   []string{"#CCCCCC"},
			Pattern: 1,
		},
	})
	if err != nil {
		return err
	}
	f.SetCellStyle("Sheet1", "A1", "D1", style)

	// Query all apartments
	rows, err := db.Query("SELECT id, owner, resident, same_flag FROM apartments ORDER BY id")
	if err != nil {
		return err
	}
	defer rows.Close()

	// Write data
	rowIndex := 2
	for rows.Next() {
		var apt Apartment
		var sameFlag int

		err = rows.Scan(&apt.ID, &apt.Owner, &apt.Resident, &sameFlag)
		if err != nil {
			return err
		}

		sameStatus := "No"
		if sameFlag == 1 {
			sameStatus = "Yes"
		}

		f.SetCellValue("Sheet1", fmt.Sprintf("A%d", rowIndex), apt.ID)
		f.SetCellValue("Sheet1", fmt.Sprintf("B%d", rowIndex), apt.Owner)
		f.SetCellValue("Sheet1", fmt.Sprintf("C%d", rowIndex), apt.Resident)
		f.SetCellValue("Sheet1", fmt.Sprintf("D%d", rowIndex), sameStatus)

		rowIndex++
	}

	// Auto-adjust column width
	for col := 'A'; col <= 'D'; col++ {
		f.SetColWidth("Sheet1", string(col), string(col), 20)
	}

	return f.SaveAs(filePath)
}
