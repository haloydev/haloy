package main

import (
	"fmt"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/configloader"
)

// We can't access private functions (applyPreset) from outside the package easily if we are in main.
// So we will just look at the exported behavior if possible or copy the logic.
// But configloader.MergeToTarget is exported!

func main() {
	fmt.Printf("None: '%s'\n", config.HistoryStrategyNone)
	fmt.Printf("Local: '%s'\n", config.HistoryStrategyLocal)

	haloyConfig := config.DeployConfig{
		Images: map[string]*config.Image{
			"my-img": {Repository: "resolved-repo"},
		},
	}
	targetConfig := config.TargetConfig{
		Preset:   config.PresetService,
		ImageKey: "my-img",
	}

	res, err := configloader.MergeToTarget(haloyConfig, targetConfig, "debug-target", "yaml")
	if err != nil {
		panic(err)
	}

	fmt.Printf("Result Image: %+v\n", res.Image)
	if res.Image.History != nil {
		fmt.Printf("Result History: %+v\n", res.Image.History)
	} else {
		fmt.Println("Result History: nil")
	}
}
