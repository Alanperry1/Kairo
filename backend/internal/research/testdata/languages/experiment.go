package main

import (
	"encoding/json"
	"os"
	"strconv"
)

func main() {
	var req struct {
		Inputs map[string]float64  `json:"inputs"`
		Rows   []map[string]string `json:"dataset_rows"`
	}
	json.NewDecoder(os.Stdin).Decode(&req)
	n, _ := strconv.ParseFloat(req.Rows[0]["value"], 64)
	sum := req.Inputs["a"] + n
	json.NewEncoder(os.Stdout).Encode(map[string]any{"kind": "computational_experiment", "metrics": map[string]any{"sum": sum}, "findings": []string{"Computed sum"}, "limitations": []string{"Test computation only"}})
}
