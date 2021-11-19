// This file is part of arduino-cli.
//
// Copyright 2020 ARDUINO SA (http://www.arduino.cc/)
//
// This software is released under the GNU General Public License version 3,
// which covers the main part of arduino-cli.
// The terms of this license can be found at:
// https://www.gnu.org/licenses/gpl-3.0.en.html
//
// You can be released from the requirements of the above licenses by purchasing
// a commercial license. Buying such a license is mandatory if you want to
// modify or otherwise use the software for commercial activities involving the
// Arduino software without disclosing the source code of your own applications.
// To purchase a commercial license, send an email to license@arduino.cc.

package arguments

import "github.com/spf13/cobra"

// Programmer contains the programmer flag data.
// This is useful so all flags used by commands that need
// this information are consistent with each other.
type Programmer struct {
	programmer string
}

// AddToCommand adds the flags used to set the programmer to the specified Command
func (p *Programmer) AddToCommand(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&p.programmer, "programmer", "P", "", tr("Programmer to use, e.g: atmel_ice"))
	cmd.RegisterFlagCompletionFunc("programmer", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return GetInstalledProgrammers(), cobra.ShellCompDirectiveDefault
	})
}

// String returns the programmer
func (p *Programmer) String() string {
	return p.programmer
}
