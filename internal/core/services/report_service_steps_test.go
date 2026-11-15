package services

import (
	"context"

	"qualflare-cli/internal/core/domain"
)

// resolveArtifactAttachments and offloadInlineAttachments run one upload step
// alone, with a refusal of its own, and print the storage-full line for that
// step; a run goes through uploadFiles, which shares one refusal between both
// steps and prints the line once.
func (s *ReportService) resolveArtifactAttachments(ctx context.Context, launch *domain.Launch) {
	refusal := &uploadRefusal{}
	s.resolveArtifacts(ctx, launch, refusal)
	s.warnStorageFull(refusal)
}

func (s *ReportService) offloadInlineAttachments(ctx context.Context, launch *domain.Launch) {
	refusal := &uploadRefusal{}
	s.offloadInline(ctx, launch, refusal)
	s.warnStorageFull(refusal)
}
