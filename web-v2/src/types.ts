export type NumericValue = number | string;
export type Nullable<T> = T | null;
export type ReaderFitMode = "" | "fit-page" | "fit-width" | "split-wide";
export type TargetType = "work" | "series";
export type MetadataOverrideField = "title" | "creator" | "series" | "language";

export interface ApiRecord {
  [key: string]: unknown;
}

export interface ListResponse<T = CatalogItem> {
  total: number;
  limit: number;
  offset: number;
  items: T[];
}

export interface ReadingProgress extends ApiRecord {
  candidate_id: string;
  work_identity_id: string;
  page_manifest_id: string;
  manifest_hash: string;
  index: number;
  count: number;
  progress_percent: number;
  completed: boolean;
  progress_status: string;
  reader_fit_mode: ReaderFitMode;
  reader_split_panel: number;
  stage_scroll_top: number;
  stage_scroll_left: number;
  updated_at: string;
  last_read_at: string;
  title: string;
}

export interface CatalogItem extends ApiRecord {
  shelf_type?: "work" | "series";
  candidate_id?: string;
  work_identity_id?: string;
  group_id?: string;
  selected_candidate_id?: string;
  title?: string;
  series_title?: string;
  display_title: string;
  display_subtitle?: string;
  display_creator?: string;
  display_library_name?: string;
  library_key?: string;
  library_name?: string;
  candidate_type?: string;
  source_kind?: string;
  extension?: string;
  relative_path?: string;
  cover_status?: string;
  cover_kind?: string;
  can_read?: boolean;
  readable_page_count?: NumericValue;
  page_count_status?: string;
  size_bytes?: NumericValue;
  added_utc?: string;
  modified_utc?: string;
  latest_modified_utc?: string;
  translation_sources?: string;
  item_role?: string;
  sequence_number?: NumericValue | null;
  item_count?: NumericValue;
  item_label?: string;
  item_summary?: string;
  section_count?: NumericValue;
  user_favorite?: boolean | NumericValue;
  user_read_status?: string;
  user_personal_rating?: number | null;
  user_reread_priority?: NumericValue;
  user_has_notes?: boolean | NumericValue;
  user_tags?: string | string[] | null;
  progress?: ReadingProgress;
  progress_page_manifest_id?: string;
  progress_manifest_hash?: string;
  progress_status?: string;
  progress_index?: NumericValue;
  progress_count?: NumericValue;
  progress_percent?: NumericValue;
  progress_completed?: boolean | NumericValue;
  progress_reader_fit_mode?: ReaderFitMode;
  progress_reader_split_panel?: NumericValue;
  progress_stage_scroll_top?: NumericValue;
  progress_stage_scroll_left?: NumericValue;
  progress_last_read_at?: string;
  progress_updated_at?: string;
}

export interface WorkSummary extends CatalogItem {
  candidate_id: string;
  work_identity_id: string;
  title: string;
}

export interface SeriesSummary extends CatalogItem {
  group_id: string;
  series_title: string;
  selected_candidate_id?: string;
  confidence?: string;
  group_path?: string;
  group_type?: string;
  series_kind?: string;
  counted_items?: NumericValue;
  counted_pages?: NumericValue;
  unique_sequence_count?: NumericValue;
  multi_section_count?: NumericValue;
  special_section_count?: NumericValue;
  manual_cover_candidate_id?: string;
}

export type ShelfItem = WorkSummary | SeriesSummary;
export type ShelfResponse = ListResponse<ShelfItem>;
export type WorksResponse = ListResponse<WorkSummary>;
export type SeriesResponse = ListResponse<SeriesSummary>;

export interface UserMark extends ApiRecord {
  target_type: TargetType;
  target_id: string;
  identity_id: string;
  read_status: string;
  read_status_client_updated_at?: string;
  personal_rating: number | null;
  favorite: boolean;
  reread_priority: number;
  translation_quality: number | null;
  image_quality: number | null;
  hidden: boolean;
  hidden_reason: string;
  notes: string;
  marked_at: string;
  updated_at: string;
}

export interface UserMarkResponse {
  target_type: TargetType;
  target_id: string;
  mark: UserMark;
}

export interface UserMarkSavePayload {
  target_type: TargetType;
  target_id: string;
  client_updated_at?: string;
  read_status?: string;
  personal_rating?: number | null;
  favorite?: boolean;
  reread_priority?: number;
  translation_quality?: number | null;
  image_quality?: number | null;
  hidden?: boolean;
  hidden_reason?: string;
  notes?: string;
}

export interface UserMarkSaveResponse extends UserMarkResponse {
  ok: boolean;
  read_status_stored?: boolean;
  reset_stored?: boolean;
  stored_fields?: string[];
  rejected_fields?: string[];
}

export interface Correction extends ApiRecord {
  target_type: string;
  target_id: string;
  correction_type: string;
  correction_value: string;
  note?: string;
  created_at?: string;
  updated_at?: string;
}

export interface CorrectionSavePayload {
  target_type: "work" | "series";
  target_id: string;
  correction_type: string;
  correction_value: string;
  note?: string;
}

export interface CorrectionSaveResponse {
  ok: boolean;
  correction: Correction;
}

export interface WorkDetail extends WorkSummary {
  path?: string;
  parent_relative_path?: string;
  page_count_reason?: string;
  metadata_overridden_fields?: string[];
  metadata_overrides?: Record<string, ApiRecord>;
}

export interface TranslationReference extends ApiRecord {
  translation_group: string;
  action: string;
  action_reason: string;
}

export interface SeriesMembership extends ApiRecord {
  group_id: string;
  series_title: string;
  item_role: string;
  sequence_number: NumericValue;
  sort_key: string;
}

export interface DoujinSeriesMembership extends ApiRecord {
  group_id: string;
  creator_display: string;
  series_title: string;
  sequence_label: string;
  sequence_kind: string;
}

export interface CreatorReference extends ApiRecord {
  creator_group_id: string;
  creator_display: string;
  parsed_title: string;
  event: string;
  parody: string;
}

export interface RelatedWorks {
  editions?: WorkSummary[];
  series: WorkSummary[];
  creators: WorkSummary[];
}

export interface TitleHints {
  creators: string[];
  series: string;
}

export interface WorkDetailResponse {
  work: WorkDetail;
  translations: TranslationReference[];
  series: SeriesMembership[];
  doujin_series: DoujinSeriesMembership[];
  creators: CreatorReference[];
  mark: UserMark | null;
  related: RelatedWorks;
  title_hints: TitleHints;
}

export interface SeriesSectionGroup extends ApiRecord {
  key: string;
  label: string;
  sort: number;
  sequence: number;
  items: WorkSummary[];
  primary: WorkSummary | null;
}

export interface SeriesSection extends ApiRecord {
  title: string;
  sort: number;
  groups: SeriesSectionGroup[];
}

export interface SeriesCoverCandidate extends WorkSummary {
  chapter_label?: string;
  cover_section_label?: string;
  cover_warning?: string;
  cover_source_path?: string;
  cover_source_relative_path?: string;
}

export interface SeriesDetailResponse {
  series: SeriesSummary;
  items: WorkSummary[];
  sections: SeriesSection[];
  sectioned: boolean;
  section_summary: string;
  cover_candidates: SeriesCoverCandidate[];
  mark: UserMark | null;
}

export interface SeriesProgressResponse {
  group_id: string;
  progress: ReadingProgress | null;
}

export interface ReadingHistoryItem extends WorkSummary {
  progress: ReadingProgress;
}

export interface ReadingHistoryResponse {
  total: number;
  items: ReadingHistoryItem[];
}

export interface ContinueTarget {
  item: WorkSummary;
  progress: ReadingProgress | null;
  series: { group_id: string; series_title: string } | null;
  next_item: WorkSummary | null;
}

export interface ContinueTargetResponse {
  target: ContinueTarget | null;
}

export interface DiscoverStats {
  favorite_count: number;
  history_count: number;
  liked_count: number;
  rated_count: number;
  reread_count: number;
}

export interface DiscoverPayload {
  total: number;
  random_mode: string;
  random_items: WorkSummary[];
  history?: ReadingHistoryItem[];
  stats?: DiscoverStats;
}

export interface MetadataOverrideEntry extends ApiRecord {
  field_name: MetadataOverrideField;
  field_value: string;
  applied_at?: string;
  updated_at?: string;
}

export interface MetadataOverrideResponse {
  ok?: boolean;
  target_type: "work";
  target_id: string;
  work_identity_id: string;
  overrides: Partial<Record<MetadataOverrideField, MetadataOverrideEntry>>;
}

export interface MetadataOverrideSavePayload {
  target_type: "work";
  target_id: string;
  field_name: MetadataOverrideField;
  field_value: string;
}

export interface DiscoverResponse extends DiscoverPayload {
  history: ReadingHistoryItem[];
  stats: DiscoverStats;
}

export interface RandomWorkResponse {
  mode: string;
  item: WorkSummary;
}

export interface PageDescriptor extends ApiRecord {
  index: number;
  relative_path: string;
  extension: string;
  size_bytes: number;
}

export interface PagesResponse {
  candidate_id: string;
  work_identity_id: string;
  page_manifest_id: string;
  manifest_hash: string;
  readable: boolean;
  count: number;
  pages: PageDescriptor[];
}

export interface ProgressResponse {
  candidate_id: string;
  work_identity_id: string;
  page_manifest_id: string;
  manifest_hash: string;
  count: number;
  progress: ReadingProgress | null;
}

export interface ProgressSavePayload {
  candidate_id: string;
  index: number;
  count: number;
  completed?: boolean;
  page_manifest_id?: string;
  manifest_hash?: string;
  reader_fit_mode?: ReaderFitMode;
  reader_split_panel?: number;
  stage_scroll_top?: number;
  stage_scroll_left?: number;
  updated_at?: string;
}

export interface ProgressSaveResponse {
  ok: boolean;
  stored: boolean;
  blocked_by_reset?: boolean;
  blocked_by_read_status?: boolean;
  timestamp_rejected?: boolean;
  rejected_reason?: string;
  discard_pending?: boolean;
  progress: ReadingProgress | null;
}

export interface BrowseQuery {
  q?: string;
  library?: string;
  type?: string;
  source?: string;
  pageStatus?: string;
  mark?: string;
  tag?: string;
  sort?: string;
  limit?: number;
  offset?: number;
}

export interface ReadingHistoryQuery {
  q?: string;
  library?: string;
  limit?: number;
}

export interface DiscoverQuery {
  q?: string;
  library?: string;
  randomMode?: string;
  randomLimit?: number;
  historyLimit?: number;
  includeHistory?: boolean | number;
  includeStats?: boolean | number;
  lean?: boolean;
}
