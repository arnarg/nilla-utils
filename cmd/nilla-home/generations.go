package main

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/arnarg/nilla-utils/internal/generation"
	"github.com/arnarg/nilla-utils/internal/util"
	"github.com/charmbracelet/lipgloss"
	"github.com/urfave/cli/v3"
)

func listGenerations(ctx context.Context, cmd *cli.Command) error {
	current, err := generation.CurrentHomeGeneration()
	if err != nil {
		return err
	}

	generations, err := generation.ListHomeGenerations()
	if err != nil {
		return err
	}

	slices.SortFunc(generations, func(a, b *generation.HomeGeneration) int {
		return cmp.Compare(b.ID, a.ID)
	})

	headers := []string{"Generation", "Build date", "Home Manager version"}
	rows := [][]string{}
	for _, gen := range generations {
		pre := " "
		if gen.ID == current.ID {
			pre = lipgloss.NewStyle().
				Foreground(lipgloss.Color("13")).
				Bold(true).
				SetString("*").
				String()
		}

		rows = append(rows, []string{
			fmt.Sprintf("%s %d", pre, gen.ID),
			gen.BuildDate.Format(time.DateTime),
			gen.Version,
		})
	}

	fmt.Println(util.RenderTable(headers, rows...))

	return nil
}
