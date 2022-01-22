package extractor

import (
	"context"
	"errors"
	"fmt"
	"strings"

	imageparser "github.com/novln/docker-parser"
	"github.com/operator-framework/operator-registry/pkg/registry"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

type MainExtractor struct {
	Log    logrus.FieldLogger
	Index  *DefaultIndexExtractor
	Bundle *DefaultBundleExtractor
}

// New - creates a new mainExtractor, with the provided options. Order of provided
// options matter, as the logger descends into both the bundle and index extractors.
func New(opts ...MainExtractorOpt) *MainExtractor {
	log := logrus.New()
	log.SetLevel(logrus.InfoLevel)

	res := &MainExtractor{
		Log:    log,
		Index:  NewIndexExtractor(),
		Bundle: NewBundleExtractor(),
	}

	for _, opt := range opts {
		opt(res)
	}
	return res
}

type MainExtractorOpt func(e *MainExtractor)

func WithIndexExtractor(indexExtractor *DefaultIndexExtractor) MainExtractorOpt {
	return func(e *MainExtractor) {
		e.Index = indexExtractor
	}
}

func WithBundleExtractor(bundleExtractor *DefaultBundleExtractor) MainExtractorOpt {
	return func(e *MainExtractor) {
		e.Bundle = bundleExtractor
	}
}

func WithLog(log logrus.FieldLogger) MainExtractorOpt {
	return func(e *MainExtractor) {
		e.Log = log
		e.Index.Log = log.WithField("source", "indexExtractor")
		e.Bundle.Log = log.WithField("source", "bundleExtractor")
	}
}

// ExtractBundles - extract bundles from indexImage matching pkgName
func (e *MainExtractor) ExtractBundles(indexImage string, pkgName string) ([]*registry.Bundle, error) {
	if err := validateIndexImage(indexImage); err != nil {
		if errors.Is(err, ErrTaglessImage) {
			e.Log.Info("skipping tagless image, nothing to extract")
			return nil, nil
		}
		e.Log.Errorf("failed to validate indexImage: %w", err)
		return nil, err
	}

	if pkgName == "" {
		err := errors.New("invalid empty pkgName")
		e.Log.Error(err)
		return nil, err
	}

	bundleImages, err := e.Index.ExtractBundleImages(indexImage, pkgName)
	if err != nil {
		e.Log.Errorf("failed to extract bundles: %w", err)
		return nil, err
	}

	return e.extractBundlesConcurrent(bundleImages)
}

// ExtractAllBundles - extract bundles for all packages from indexImage
func (e *MainExtractor) ExtractAllBundles(indexImage string) ([]*registry.Bundle, error) {
	if err := validateIndexImage(indexImage); err != nil {
		if errors.Is(err, ErrTaglessImage) {
			e.Log.Info("skipping tagless image, nothing to extract")
			return nil, nil
		}
		e.Log.Errorf("failed to validate indexImage: %w", err)
		return nil, err
	}

	bundleImages, err := e.Index.ExtractAllBundleImages(indexImage)
	if err != nil {
		e.Log.Errorf("failed to extract all bundles: %w", err)
		return nil, err
	}

	return e.extractBundlesConcurrent(bundleImages)
}

func (e *MainExtractor) extractBundlesConcurrent(bundleImages []string) ([]*registry.Bundle, error) {
	res := make([]*registry.Bundle, len(bundleImages))
	g := new(errgroup.Group)

	// we need the global context to be able to cancel all goroutines
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for i, bundleImage := range bundleImages {
		i, bundleImage := i, bundleImage // https://golang.org/doc/faq#closures_and_goroutines
		g.Go(func() error {
			bundle, err := e.Bundle.Extract(ctx, bundleImage)
			if err == nil {
				res[i] = bundle
			}
			return err
		})
	}
	// blocks until all calls to `g.Go` have completed
	// first non-nil error cancels the group
	if err := g.Wait(); err != nil {
		cancel() // cancels all running goroutines
		return nil, err
	}
	return res, nil
}

func validateIndexImage(indexImage string) error {
	if indexImage == "" {
		return errors.New("invalid empty indexImage")
	}
	if err := isTaglessImage(indexImage); err != nil {
		return err
	}
	if _, err := imageparser.Parse(indexImage); err != nil {
		return fmt.Errorf("can't parse indexImage '%s', got %w", indexImage, err)
	}
	return nil
}

var ErrTaglessImage = errors.New("indexImage is tagless, skipping the addon as it is not onboarded")

// (sblaisdo) ignore tagless images used by addons in the process of on-boarding
// TODO - Modify how we detect that an addon is in the on-boarding state. This
// method is too cryptic.
func isTaglessImage(indexImage string) error {
	parts := strings.SplitN(indexImage, ":", 2)
	if len(parts) < 2 {
		return ErrTaglessImage
	}
	return nil
}
