package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/a-tal/esi-isk/isk/cx"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

// Affiliation links a character with a corporation and maybe alliance
type Affiliation struct {
	Character   *Name
	Corporation *Name
	Alliance    *Name
}

// Character describes the output format of known characters
type Character struct {
	// ID is the characterID of this donator/recipient
	ID int32 `json:"id"`

	// Name is the last checked name of the character
	Name string `json:"name,omitempty"`

	// CorporationID is the last checked corporation ID of the character
	CorporationID int32 `json:"corporation,omitempty"`

	// CorporationName is the last checked name of the corporation
	CorporationName string `json:"corporation_name,omitempty"`

	// AllianceID is the last checked alliance ID of the character
	AllianceID int32 `json:"alliance,omitempty"`

	// AllianceName is the last checked name of the alliance
	AllianceName string `json:"alliance_name,omitempty"`

	// Received donations and/or contracts
	Received int64 `json:"received,omitempty"`

	// ReceivedISK value of all donations plus contracts
	ReceivedISK float64 `json:"received_isk,omitempty"`

	// Received donations and/or contracts in the last 30 days
	Received30 int64 `json:"received_30,omitempty"`

	// ReceivedISK30 value of all donations plus contracts in the last 30 days
	ReceivedISK30 float64 `json:"received_isk_30,omitempty"`

	// Donated is the number of times this character has donated to someone else
	Donated int64 `json:"donated,omitempty"`

	// DonatedISK is the value of all ISK donated
	DonatedISK float64 `json:"donated_isk,omitempty"`

	// Donated30 is the number of donations in the last 30 days
	Donated30 int64 `json:"donated_30,omitempty"`

	// DonatedISK30 is the value of all ISK donated in the last 30 days
	DonatedISK30 float64 `json:"donated_isk_30,omitempty"`

	// LastDonated timestamp
	LastDonated time.Time `json:"last_donated,omitempty"`

	// LastReceived timestamp
	LastReceived time.Time `json:"last_received,omitempty"`

	// GoodStanding boolean
	GoodStanding bool `json:"good_standing"`
}

// MarshalJSON implementation to omit our null timestamps
func (c *Character) MarshalJSON() ([]byte, error) {
	type Alias Character

	var lastDonatedStr string
	var lastReceivedStr string

	if !c.LastDonated.IsZero() {
		lastDonatedStr = c.LastDonated.Format(time.RFC3339)
	}
	if !c.LastReceived.IsZero() {
		lastReceivedStr = c.LastReceived.Format(time.RFC3339)
	}

	return json.Marshal(&struct {
		*Alias
		LastReceived string `json:"last_received,omitempty"`
		LastDonated  string `json:"last_donated,omitempty"`
	}{
		Alias:        (*Alias)(c),
		LastReceived: lastReceivedStr,
		LastDonated:  lastDonatedStr,
	})
}

// CharacterRow describes Character as stored in the characters table
type CharacterRow struct {
	// ID is the characterID of this donator/recipient
	ID int32 `db:"character_id"`

	// CorporationID is the last checked corporation ID of the character
	CorporationID int32 `db:"corporation_id"`

	// AllianceID is the last checked alliance ID of the character
	AllianceID int32 `db:"alliance_id"`

	// Received donations and/or contracts
	Received int64 `db:"received"`

	// ReceivedISK value of all donations plus contracts
	ReceivedISK float64 `db:"received_isk"`

	// Received donations and/or contracts in the last 30 days
	Received30 int64 `db:"received_30"`

	// ReceivedISK30 value of all donations plus contracts in the last 30 days
	ReceivedISK30 float64 `db:"received_isk_30"`

	// Donated is the number of times this character has donated to someone else
	Donated int64 `db:"donated"`

	// DonatedISK is the value of all ISK donated
	DonatedISK float64 `db:"donated_isk"`

	// Donated30 is the number of donations in the last 30 days
	Donated30 int64 `db:"donated_30"`

	// DonatedISK30 is the value of all ISK donated in the last 30 days
	DonatedISK30 float64 `db:"donated_isk_30"`

	// LastDonated timestamp
	LastDonated pq.NullTime `db:"last_donated"`

	// LastReceived timestamp
	LastReceived pq.NullTime `db:"last_received"`

	// GoodStanding boolean
	GoodStanding bool `db:"good_standing"`
}

// CharDetails is the api return for a character
type CharDetails struct {
	Character *Character `json:"character"`

	// ISK IN
	Donations Donations `json:"donations,omitempty"`
	Contracts Contracts `json:"contracts,omitempty"`

	// ISK OUT
	Donated    Donations `json:"donated,omitempty"`
	Contracted Contracts `json:"contracted,omitempty"`
}

// GetCharDetails returns details for the character from pg
func GetCharDetails(ctx context.Context, charID int32) (*CharDetails, error) {
	char, err := GetCharacter(ctx, charID)
	if err != nil {
		return nil, err
	}

	contracts, err := getCharContracts(ctx, charID)
	if err != nil {
		return nil, err
	}

	contracted, err := getCharContracted(ctx, charID)
	if err != nil {
		return nil, err
	}

	donations, err := GetCharDonations(ctx, charID)
	if err != nil {
		return nil, err
	}

	donated, err := GetCharDonated(ctx, charID)
	if err != nil {
		return nil, err
	}

	details := &CharDetails{
		Character:  char,
		Donations:  donations,
		Donated:    donated,
		Contracts:  contracts,
		Contracted: contracted,
	}

	return details, nil
}

func getAffiliation(charID int32, affiliations []*Affiliation) *Affiliation {
	for _, aff := range affiliations {
		if aff.Character.ID == charID {
			return aff
		}
	}
	// this should never happen
	panic(fmt.Errorf("no affiliation found for character %d", charID))
}

// SaveCharacterDonations updates all totals in the characters table
func SaveCharacterDonations(
	ctx context.Context,
	donations []*Donation,
	affiliations []*Affiliation,
	addition bool,
) error {
	newCharacters := []*CharacterRow{}
	updatedCharacters := []*CharacterRow{}
	allCharacters := []int32{}

	for _, donation := range donations {
		for _, charID := range []int32{donation.Donator, donation.Recipient} {
			if inInt32(charID, allCharacters) {
				continue
			}
			allCharacters = append(allCharacters, charID)

			char, new := bindAffiliation(ctx, charID, affiliations)
			if new {
				newCharacters = append(newCharacters, char)
			} else {
				updatedCharacters = append(updatedCharacters, char)
			}
		}

		if addition {
			addToTotals(donation, newCharacters, updatedCharacters)
		} else {
			removeFromTotals(donation, newCharacters, updatedCharacters)
		}

	}

	return saveCharacters(ctx, newCharacters, updatedCharacters)
}

// SaveCharacterContracts updates all totals in the characters table
func SaveCharacterContracts(
	ctx context.Context,
	donations Contracts,
	affiliations []*Affiliation,
	addition bool,
) error {
	newCharacters := []*CharacterRow{}
	updatedCharacters := []*CharacterRow{}
	allCharacters := []int32{}

	for _, contract := range donations {
		if !contract.Accepted {
			continue
		}
		for _, charID := range []int32{contract.Donator, contract.Receiver} {
			if inInt32(charID, allCharacters) {
				continue
			}
			allCharacters = append(allCharacters, charID)

			char, new := bindAffiliation(ctx, charID, affiliations)
			if new {
				newCharacters = append(newCharacters, char)
			} else {
				updatedCharacters = append(updatedCharacters, char)
			}
		}

		if addition {
			addToContractTotals(contract, newCharacters, updatedCharacters)
		} else {
			removeFromContractTotals(contract, newCharacters, updatedCharacters)
		}
	}

	return saveCharacters(ctx, newCharacters, updatedCharacters)
}

// SaveCharacter saves a single character
func SaveCharacter(ctx context.Context, char *Character) error {
	return updateCharacter(ctx, char.toRow())
}

func saveCharacters(
	ctx context.Context,
	newCharacters, updatedCharacters []*CharacterRow,
) error {
	failedChars := []string{}
	for _, char := range newCharacters {
		if err := NewCharacter(ctx, char); err != nil {
			log.Printf("failed to save new character %d: %+v", char.ID, err)
			failedChars = append(failedChars, fmt.Sprintf("%d", char.ID))
		}
	}

	for _, char := range updatedCharacters {
		if err := updateCharacter(ctx, char); err != nil {
			log.Printf("failed to save updated character %d: %+v", char.ID, err)
			failedChars = append(failedChars, fmt.Sprintf("%d", char.ID))
		}
	}

	if len(failedChars) > 0 {
		return fmt.Errorf(
			"failed to save character(s): %s",
			strings.Join(failedChars, ", "),
		)
	}

	return nil
}

func bindAffiliation(
	ctx context.Context,
	charID int32,
	affiliations []*Affiliation,
) (row *CharacterRow, new bool) {
	aff := getAffiliation(charID, affiliations)

	char, err := GetCharacter(ctx, charID)

	if err != nil {
		new = true
		row = &CharacterRow{ID: charID}
	} else {
		row = char.toRow()
	}

	row.CorporationID = aff.Corporation.ID
	if aff.Alliance != nil {
		row.AllianceID = aff.Alliance.ID
	}

	return row, new
}

// addToTotals adds donation/received totals
func addToTotals(donation *Donation, characters ...[]*CharacterRow) {
	for _, chars := range characters {
		for _, char := range chars {
			if char.ID == donation.Donator {
				char.DonatedISK += donation.Amount
				char.Donated++
				char.DonatedISK30 += donation.Amount
				char.Donated30++
				if !char.LastDonated.Valid || char.LastDonated.Time.Before(
					donation.Timestamp) {
					char.LastDonated = pq.NullTime{Time: donation.Timestamp, Valid: true}
					char.LastDonated.Valid = true
				}
			} else if char.ID == donation.Recipient {
				char.ReceivedISK += donation.Amount
				char.Received++
				char.ReceivedISK30 += donation.Amount
				char.Received30++
				if !char.LastReceived.Valid || char.LastReceived.Time.Before(
					donation.Timestamp) {
					char.LastReceived = pq.NullTime{Time: donation.Timestamp, Valid: true}
					char.LastReceived.Valid = true
				}
			}
		}
	}
}

// removeFromTotals removes donation/received totals (from 30 day)
func removeFromTotals(donation *Donation, characters ...[]*CharacterRow) {
	for _, chars := range characters {
		for _, char := range chars {
			if char.ID == donation.Donator {
				char.DonatedISK30 -= donation.Amount
				char.Donated30--
			} else if char.ID == donation.Recipient {
				char.ReceivedISK30 -= donation.Amount
				char.Received30--
			}
		}
	}
}

// NewCharacter adds a new character to the characters table
func NewCharacter(ctx context.Context, char *CharacterRow) error {
	return executeChar(ctx, char, cx.StmtCreateCharacter)
}

// updateCharacter updates a character in the characters table
func updateCharacter(ctx context.Context, char *CharacterRow) error {
	return executeChar(ctx, char, cx.StmtUpdateCharacter)
}

// executeChar is a DRY helper to create or update a character
func executeChar(ctx context.Context, char *CharacterRow, key cx.Key) error {
	return executeNamed(ctx, key, map[string]interface{}{
		"character_id":    char.ID,
		"corporation_id":  char.CorporationID,
		"alliance_id":     char.AllianceID,
		"received":        char.Received,
		"received_isk":    char.ReceivedISK,
		"received_30":     char.Received30,
		"received_isk_30": char.ReceivedISK30,
		"donated":         char.Donated,
		"donated_isk":     char.DonatedISK,
		"donated_30":      char.Donated30,
		"donated_isk_30":  char.DonatedISK30,
		"last_donated":    char.LastDonated,
		"last_received":   char.LastReceived,
		"good_standing":   char.GoodStanding,
	})
}

// GetCharacter pulls a single character from the db
func GetCharacter(ctx context.Context, charID int32) (*Character, error) {
	rows, err := queryNamedResult(
		ctx,
		cx.StmtCharDetails,
		map[string]interface{}{"character_id": charID},
	)

	if err != nil {
		return nil, err
	}

	charRow, err := scanCharacterRow(rows)
	if err != nil {
		return nil, err
	}

	char, err := getCharacterNames(ctx, charRow)
	if err != nil {
		return nil, err
	}

	return char, nil
}

func scanCharacterRow(rows *sqlx.Rows) (*CharacterRow, error) {
	res, err := scan(rows, func() interface{} { return &CharacterRow{} })
	if err != nil {
		return nil, err
	}
	for _, i := range res {
		return i.(*CharacterRow), nil
	}

	return nil, errors.New("character not found")
}

// getCharacterNames fills in the character, corporation and alliance names
func getCharacterNames(
	ctx context.Context,
	row *CharacterRow,
) (*Character, error) {
	ids := []int32{}
	for _, id := range []int32{row.ID, row.CorporationID, row.AllianceID} {
		if id > 0 {
			ids = append(ids, id)
		}
	}

	names, err := GetNames(ctx, ids...)
	if err != nil {
		return nil, err
	}

	char := row.toCharacter()

	for id, name := range names {
		if id == char.ID {
			char.Name = name
		} else if id == char.CorporationID {
			char.CorporationName = name
		} else if id == char.AllianceID {
			char.AllianceName = name
		} else {
			log.Printf("pulled unknown ID: %d, name: %s", id, name)
		}
	}

	return char, nil
}

func (c *CharacterRow) toCharacter() *Character {
	char := &Character{
		ID:            c.ID,
		CorporationID: c.CorporationID,
		AllianceID:    c.AllianceID,
		Received:      c.Received,
		ReceivedISK:   round2(c.ReceivedISK),
		Received30:    c.Received30,
		ReceivedISK30: round2(c.ReceivedISK30),
		Donated:       c.Donated,
		DonatedISK:    round2(c.DonatedISK),
		Donated30:     c.Donated30,
		DonatedISK30:  round2(c.DonatedISK30),
		GoodStanding:  c.GoodStanding,
	}
	if c.LastDonated.Valid {
		char.LastDonated = c.LastDonated.Time
	}
	if c.LastReceived.Valid {
		char.LastReceived = c.LastReceived.Time
	}
	return char
}

func (c *Character) toRow() *CharacterRow {
	return &CharacterRow{
		ID:            c.ID,
		CorporationID: c.CorporationID,
		AllianceID:    c.AllianceID,
		Received:      c.Received,
		ReceivedISK:   c.ReceivedISK,
		Received30:    c.Received30,
		ReceivedISK30: c.ReceivedISK30,
		Donated:       c.Donated,
		DonatedISK:    c.DonatedISK,
		Donated30:     c.Donated30,
		DonatedISK30:  c.DonatedISK30,
		LastDonated: pq.NullTime{
			Time:  c.LastDonated,
			Valid: !c.LastDonated.IsZero(),
		},
		LastReceived: pq.NullTime{
			Time:  c.LastReceived,
			Valid: !c.LastReceived.IsZero(),
		},
		GoodStanding: c.GoodStanding,
	}
}
