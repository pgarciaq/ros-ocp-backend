package cmd

import (
	"encoding/csv"
	"fmt"
	"log"
	"os"

	"github.com/go-gota/gota/dataframe"
	"github.com/spf13/cobra"

	"github.com/redhatinsights/ros-ocp-backend/internal/types"
	"github.com/redhatinsights/ros-ocp-backend/internal/utils"
)

var (
	outputDir     string
	aggregatorCmd = &cobra.Command{
		Use:   "aggregator [input csv file path]",
		Short: "aggregates CSV data",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			input_file := args[0]
			if _, err := os.Stat(input_file); os.IsNotExist(err) {
				log.Fatalf("CSV file: %s does not exist", input_file)
			}
			if outputDir != "" {
				if _, err := os.Stat(outputDir); os.IsNotExist(err) {
					if err := os.MkdirAll(outputDir, os.ModePerm); err != nil {
						log.Fatalf("cannot create output directory: %v", err)
					}
				}
			} else {
				outputDir, _ = os.Getwd()
			}
			outputFile := outputDir + "/output.csv"
			f, err := os.Open(input_file)
			if err != nil {
				log.Fatalf("cannot open input file: %v", err)
			}
			defer func() {
				_ = f.Close()
			}()

			csv := csv.NewReader(f)
			records, err := csv.ReadAll()
			if err != nil {
				log.Fatalf("cannot read CSV: %v", err)
			}
			csvType := utils.DetermineCSVType(input_file)
			columnHeaders := types.GetColumnMapping(csvType)
			df := dataframe.LoadRecords(records, dataframe.WithTypes(columnHeaders))
			df, err = utils.Aggregate_data(csvType, df)
			if err != nil {
				log.Fatalf("aggregation failed: %v", err)
			}
			fileio, err := os.Create(outputFile)
			if err != nil {
				log.Fatalf("cannot create output file: %v", err)
			}
			if err := df.WriteCSV(fileio); err != nil {
				log.Fatalf("cannot write CSV: %v", err)
			}
			fmt.Printf("Aggregated CSV created at: %s \n", outputFile)
		},
	}
)

func init() {
	aggregatorCmd.PersistentFlags().StringVarP(&outputDir, "output-dir", "o", "", "Path to output directory")
	rootCmd.AddCommand(aggregatorCmd)
}
