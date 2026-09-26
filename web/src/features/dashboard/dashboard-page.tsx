import { useEffect, useLayoutEffect, useRef, useState } from "react";
import type { CSSProperties, KeyboardEvent as ReactKeyboardEvent, PointerEvent as ReactPointerEvent } from "react";
import { Link, Navigate, useLocation, useNavigate, useParams } from "react-router";
import {
  ArrowLeft,
  ArrowUpRight,
  Bell,
  CircleHelp,
  FolderOpen,
  Home,
  LogOut,
  Moon,
  MessageCircle,
  Pin,
  Search,
  Sparkles,
  Send,
  Settings,
  Sun,
  FileVideo,
  LoaderCircle,
  RotateCcw,
  RefreshCw,
  Radio,
  Check,
  CheckSquare2,
  CheckCircle2,
  CircleCheck,
  ChevronDown,
  ScanSearch,
  Square,
  Trash2,
  X,
  Link2,
  Network,
  ShieldCheck,
} from "lucide-react";
import { Brand } from "../../components/ui/brand";
import { MediaSettingsPanel } from "../settings/media-settings-panel";
import { AdminAccountPanel } from "../settings/admin-account-panel";
import { MirrorPanel } from "../mirror/mirror-panel";

type Profile = {
  first_name: string;
  last_name: string;
  username?: string;
  phone: string;
  display_name: string;
  user_id: string;
  connected: boolean;
  avatar_url?: string;
};
type GlobalSearchResult = {
  kind: "file" | "topic" | "group";
  id: string; group_id: string; topic_id?: string; video_id?: string;
  title: string; detail?: string; group_title: string; topic_title?: string;
  media_type?: "movie" | "tv"; official_title?: string; original_title?: string;
  year?: string; poster_path?: string; matched: boolean; exported: boolean; byte_size?: number; remote?: boolean; importable?: boolean;
  remote_peer_type?: string; remote_peer_id?: number; remote_message_id?: number; remote_topic_id?: number;
};
type ImportReviewSelection = {
  message_ids_by_topic?: Record<string, string[]>;
  match_queries_by_topic?: Record<string, string>;
};
type Group = {
  id: string;
  title: string;
  username?: string;
  forum: boolean;
  active: boolean;
};
type Topic = {
  id: string;
  telegram_id: number;
  title: string;
  icon_color?: number;
  icon_emoji_id?: string;
  closed: boolean;
  hidden: boolean;
  pinned: boolean;
  unread_count: number;
  unread_mentions_count: number;
  top_message_id: number;
  video_count: number;
  total_bytes: number;
  exported_count: number;
  unresolved_count: number;
  library_kind: "unclassified" | "movie_collection" | "series" | "ignored";
};
type TopicResponse = {
  group: Group;
  topics: Topic[];
  sync: "ready" | "stale";
};
type TopicMessage = {
  id: string;
  telegram_id: number;
  body: string;
  media_type?: string;
  sent_at: string;
  edited_at?: string;
  outgoing: boolean;
  pinned: boolean;
  file_name: string;
  mime_type: string;
  byte_size: number;
  width?: number;
  height?: number;
  duration_seconds?: number;
  export_error?: string;
  season_number?: number;
  episode_number?: number;
};
type MessageResponse = {
  topic: Topic;
  messages: TopicMessage[];
  has_more: boolean;
  next_cursor: number;
  sync: "ready" | "stale";
};
type VideoDetail = Omit<TopicMessage, "body" | "telegram_id"> & {
  caption: string;
  telegram_message_id: number;
};
type VideoResponse = { group: Group; topic: Topic; video: VideoDetail };
type TMDBResult = {
  id: number;
  title: string;
  original_title: string;
  release_date: string;
  poster_path?: string;
  overview: string;
};
type TMDBMatch = TMDBResult & {
  match_id: string;
  media_type: "movie" | "tv";
  tmdb_id: number;
  message_id: string;
};
type TopicArtwork = { topic_id: string; kind: "show" | "collection"; title?: string; year?: string; backdrop_path?: string; poster_paths: string[] };
type GroupVideoScan = { status: "idle" | "running" | "ready" | "stale"; total_topics: number; completed_topics: number; video_count: number; total_bytes: number; failed_topics: number; last_error?: string };
type ImportDestination = { id: string; title: string; library_kind: string };
type ImportPreview = { topic_id: string; source_title: string; topic_title?: string; video_count: number; suggested_id: number; candidates: TMDBResult[]; media_type?: "tv" | "movie"; destinations?: ImportDestination[]; preparation_status: "idle" | "running" | "ready" | "failed"; preparation_error?: string };
type ImportStatus = { status: "idle" | "pending" | "running" | "completed" | "failed"; destination_title?: string; requested?: number; completed?: number; failed?: number; last_error?: string };
type ActivityChild = { id: string; status: string; detail: string; occurred_at: string };
type ActivityItem = { id: string; kind: "import" | "mirror" | "strm" | "review" | "topic_deleted" | "episode_replaced" | "backup"; status: string; title: string; detail: string; occurred_at: string; href?: string; children?: ActivityChild[] };
type JobItem = { id: string; kind: "import" | "mirror" | "strm" | "scan"; status: string; title: string; detail: string; updated_at: string; href?: string };
type JobsResponse = { items: JobItem[]; recent_completed: JobItem[] };
type NotificationLevel = "strict" | "balanced" | "essential";
type EpisodeResolution = {
  series: { tmdb_id: number; title: string };
  saved: { exists: boolean; season: number; episode: number; title: string };
  suggested: { detected: boolean; season: number; episode: number };
};
type EpisodeOption = { number: number; name: string; air_date?: string; overview?: string; still_path?: string };
type MissingEpisode = { season: number; episode: number; title: string; air_date?: string; still_path?: string };

const formatBytes = (bytes: number) => {
  if (!bytes) return "Tamaño desconocido";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  return `${(bytes / 1024 ** index).toLocaleString("es", { maximumFractionDigits: index > 2 ? 2 : 1 })} ${units[index]}`;
};

const formatDuration = (seconds?: number) => {
  if (!seconds) return "Duración desconocida";
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const rest = seconds % 60;
  return [hours, minutes, rest].map((part, index) => index === 0 ? String(part) : String(part).padStart(2, "0")).join(":");
};

export function DashboardPage({ mode = "catalog" }: { mode?: "catalog" | "settings" | "manage" | "import" | "activity" | "mirror" | "mirror-manage" }) {
  const location = useLocation();
  const navigate = useNavigate();
  const { groupId, topicId, topicIds, videoId } = useParams();
  const [workspaceMode, setWorkspaceMode] = useState<"catalog" | "manage">(() =>
    sessionStorage.getItem("tgstrm.workspace-mode") === "manage" ? "manage" : "catalog",
  );
  const activeMode = mode === "settings" ? "settings" : mode === "activity" ? "activity" : mode === "mirror" || mode === "mirror-manage" ? "mirror" : mode === "manage" || mode === "import" ? "manage" : workspaceMode;
  const [profile, setProfile] = useState<Profile>();
  const [failed, setFailed] = useState(false);
  const [dark, setDark] = useState(false);
  const [logoutOpen, setLogoutOpen] = useState(false);
  const [logoutBusy, setLogoutBusy] = useState(false);
  const [logoutError, setLogoutError] = useState("");
  const [groups, setGroups] = useState<Group[]>([]);
  const [groupDetail, setGroupDetail] = useState<Group>();
  const [topics, setTopics] = useState<Topic[]>([]);
  const [topicDetail, setTopicDetail] = useState<Topic>();
  const [messages, setMessages] = useState<TopicMessage[]>([]);
  const [messageState, setMessageState] = useState<"loading" | "ready" | "stale" | "missing">("ready");
  const [nextCursor, setNextCursor] = useState(0);
  const [hasMoreMessages, setHasMoreMessages] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [videoDetail, setVideoDetail] = useState<VideoResponse>();
  const [videoState, setVideoState] = useState<"loading" | "ready" | "missing">("ready");
  const [playerState, setPlayerState] = useState<"loading" | "ready" | "error">("loading");
  const [playerAttempt, setPlayerAttempt] = useState(0);
  const [catalogState, setCatalogState] = useState<"loading" | "ready" | "stale" | "missing">(
    groupId ? "loading" : "ready",
  );
  const [groupQuery, setGroupQuery] = useState("");
  const [selectedTopicIds, setSelectedTopicIds] = useState<Set<string>>(new Set());
  const [selectedMessageIds, setSelectedMessageIds] = useState<Set<string>>(new Set());
  const [selectingTopics, setSelectingTopics] = useState(false);
  const [selectingVideos, setSelectingVideos] = useState(false);
  const [exportState, setExportState] = useState("");
  const [importPreview, setImportPreview] = useState<ImportPreview[]>([]);
  const [importChoices, setImportChoices] = useState<Record<string, number>>({});
  const [importMediaTypes, setImportMediaTypes] = useState<Record<string, "tv" | "movie">>({});
  const [importDestinations, setImportDestinations] = useState<Record<string, string>>({});
  const [importQueries, setImportQueries] = useState<Record<string, string>>({});
  const [importSearching, setImportSearching] = useState<string>();
  const [importStatuses, setImportStatuses] = useState<Record<string, ImportStatus>>({});
  const [importReviewState, setImportReviewState] = useState<"loading" | "ready" | "sending" | "error">("loading");
  const [importRefreshNonce, setImportRefreshNonce] = useState(0);
  const [tmdbImageBase, setTMDBImageBase] = useState("https://image.tmdb.org/t/p/w342");
  const [tmdbMatches, setTMDBMatches] = useState<TMDBMatch[]>([]);
  const [tmdbScanProgress, setTMDBScanProgress] = useState<{ running: boolean; total: number; matched: number; reviewed: number }>();
  const [tmdbInitialScanFinished, setTMDBInitialScanFinished] = useState(false);
  const [topicArtwork, setTopicArtwork] = useState<Record<string, TopicArtwork>>({});
  const [groupVideoScan, setGroupVideoScan] = useState<GroupVideoScan>();
  const [scanPollNonce, setScanPollNonce] = useState(0);
  const [activity, setActivity] = useState<ActivityItem[]>([]);
  const [notificationActivity, setNotificationActivity] = useState<ActivityItem[]>([]);
  const [jobs, setJobs] = useState<JobItem[]>([]);
  const [recentJobs, setRecentJobs] = useState<JobItem[]>([]);
  const [retryingJob, setRetryingJob] = useState<string>();
  const [jobRetryError, setJobRetryError] = useState("");
  const [activityRefreshNonce, setActivityRefreshNonce] = useState(0);
  const [notificationLevel, setNotificationLevel] = useState<NotificationLevel>("balanced");
  const [notificationsOpen, setNotificationsOpen] = useState(false);
  const [globalSearchOpen, setGlobalSearchOpen] = useState(false);
  const [globalSearchQuery, setGlobalSearchQuery] = useState("");
  const [globalSearchResults, setGlobalSearchResults] = useState<GlobalSearchResult[]>([]);
  const [globalSearchState, setGlobalSearchState] = useState<"idle"|"loading"|"ready"|"error">("idle");
  const [globalSearchSelected, setGlobalSearchSelected] = useState(0);
  const [remoteIndexing, setRemoteIndexing] = useState<string>();
  const [remoteIndexError, setRemoteIndexError] = useState("");
  const [accountOpen, setAccountOpen] = useState(false);
  const [adminUsername, setAdminUsername] = useState("admin");
  const [notificationsReadAt, setNotificationsReadAt] = useState(() => localStorage.getItem("tgstrm.notifications-read-at") || "");
  const [expandedActivity, setExpandedActivity] = useState<Set<string>>(new Set());
  const [resolutionTarget, setResolutionTarget] = useState<TopicMessage>();
  const [resolutionInfo, setResolutionInfo] = useState<EpisodeResolution>();
  const [resolutionSeason, setResolutionSeason] = useState(1);
  const [resolutionEpisode, setResolutionEpisode] = useState(0);
  const [episodeOptions, setEpisodeOptions] = useState<EpisodeOption[]>([]);
  const [missingEpisodes, setMissingEpisodes] = useState<MissingEpisode[]>([]);
  const [resolutionState, setResolutionState] = useState<"idle" | "loading" | "episodes" | "saving">("idle");
  const [resolutionError, setResolutionError] = useState("");
  const [topicMatchOpen, setTopicMatchOpen] = useState(false);
  const [topicMatchQuery, setTopicMatchQuery] = useState("");
  const [topicMatchResults, setTopicMatchResults] = useState<TMDBResult[]>([]);
  const [topicMatchState, setTopicMatchState] = useState<"idle" | "searching" | "saving">("idle");
  const [topicMatchError, setTopicMatchError] = useState("");
  const [movieMatchTarget, setMovieMatchTarget] = useState<TopicMessage>();
  const [movieMatchQuery, setMovieMatchQuery] = useState("");
  const [movieMatchResults, setMovieMatchResults] = useState<TMDBResult[]>([]);
  const [movieMatchState, setMovieMatchState] = useState<"idle" | "searching" | "saving">("idle");
  const [movieMatchError, setMovieMatchError] = useState("");
  const [tmdbBackdropBase, setTMDBBackdropBase] = useState("https://image.tmdb.org/t/p/w780");
  const [mobileCondensed, setMobileCondensed] = useState(false);
  const [draggingScroll, setDraggingScroll] = useState(false);
  const frameRef = useRef<HTMLElement>(null);
  const topicCatalogCompleteRef = useRef(false);
  const notificationAreaRef = useRef<HTMLDivElement>(null);
  const accountAreaRef = useRef<HTMLDivElement>(null);
  const globalSearchInputRef = useRef<HTMLInputElement>(null);
  const scrollEdgeRef = useRef<HTMLSpanElement>(null);
  const dragOffsetRef = useRef<number | undefined>(undefined);
  const [scrollIndicator, setScrollIndicator] = useState({
    visible: false,
    height: 0,
    top: 0,
  });

  useLayoutEffect(() => {
    const frame = frameRef.current;
    if (frame) frame.scrollTop = 0;
    setMobileCondensed(false);
    setDraggingScroll(false);
    dragOffsetRef.current = undefined;
  }, [location.pathname]);

  useEffect(() => {
    setPlayerState("loading");
    setPlayerAttempt(0);
  }, [videoId]);

  useEffect(() => {
	let stopped = false;
    const refresh = () => Promise.all([
      fetch("/api/v1/activity", { credentials: "same-origin" }),
      fetch("/api/v1/activity?scope=notifications", { credentials: "same-origin" }),
      fetch("/api/v1/jobs", { credentials: "same-origin" }),
    ])
	  .then(async ([activityResponse, notificationResponse, jobsResponse]) => {
        if (!activityResponse.ok || !notificationResponse.ok || !jobsResponse.ok) throw new Error();
        return Promise.all([activityResponse.json() as Promise<{ items: ActivityItem[] }>, notificationResponse.json() as Promise<{ items: ActivityItem[] }>, jobsResponse.json() as Promise<JobsResponse>]);
      })
      .then(([activityData, notificationData, jobsData]) => { if (!stopped) { setActivity(activityData.items); setNotificationActivity(notificationData.items); setJobs(jobsData.items); setRecentJobs(jobsData.recent_completed || []); } })
	  .catch(() => undefined);
	void refresh();
	const timer = window.setInterval(refresh, 30_000);
	return () => { stopped = true; window.clearInterval(timer); };
  }, [activityRefreshNonce]);

  useEffect(() => {
    fetch("/api/v1/settings/media", { credentials: "same-origin" })
      .then((response) => response.ok ? response.json() as Promise<{ notification_level?: NotificationLevel }> : Promise.reject())
      .then((data) => setNotificationLevel(data.notification_level || "balanced"))
      .catch(() => undefined);
  }, [mode]);

  useEffect(() => {
    if (!notificationsOpen) return;
    const closeOutside = (event: PointerEvent) => {
      if (!notificationAreaRef.current?.contains(event.target as Node)) setNotificationsOpen(false);
    };
    document.addEventListener("pointerdown", closeOutside);
    return () => document.removeEventListener("pointerdown", closeOutside);
  }, [notificationsOpen]);

  useEffect(() => {
    fetch("/api/v1/admin/session", { credentials: "same-origin" }).then(r=>r.json()).then(data=>{if(data.username)setAdminUsername(data.username)}).catch(()=>undefined);
  }, []);
  useEffect(() => {
    if (!accountOpen) return;
    const closeOutside=(event:PointerEvent)=>{if(!accountAreaRef.current?.contains(event.target as Node))setAccountOpen(false)};
    document.addEventListener("pointerdown",closeOutside);return()=>document.removeEventListener("pointerdown",closeOutside);
  }, [accountOpen]);

  useEffect(() => {
    if (!globalSearchOpen) return;
    const frame = window.requestAnimationFrame(() => globalSearchInputRef.current?.focus());
    const close = (event: KeyboardEvent) => { if (event.key === "Escape") setGlobalSearchOpen(false); };
    document.addEventListener("keydown", close);
    return () => { window.cancelAnimationFrame(frame); document.removeEventListener("keydown", close); };
  }, [globalSearchOpen]);

  useEffect(() => {
    const query = globalSearchQuery.trim();
    setGlobalSearchSelected(0);
    if (query.length < 2) { setGlobalSearchResults([]); setGlobalSearchState("idle"); return; }
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      setGlobalSearchState("loading");
      fetch(`/api/v1/search?q=${encodeURIComponent(query)}&limit=24`, { credentials:"same-origin", signal:controller.signal })
        .then(async response => { if(!response.ok) throw new Error(); return response.json() as Promise<{items:GlobalSearchResult[]}>; })
        .then(data => { setGlobalSearchResults(data.items); setGlobalSearchState("ready"); })
        .catch(error => { if(error?.name!=="AbortError") setGlobalSearchState("error"); });
    }, 250);
    return () => { window.clearTimeout(timer); controller.abort(); };
  }, [globalSearchQuery]);

  const searchResultHref = (item: GlobalSearchResult) => item.kind === "group" ? `/groups/${item.group_id}` : item.kind === "topic" ? `/groups/${item.group_id}/topics/${item.topic_id}` : `/groups/${item.group_id}/topics/${item.topic_id}/videos/${item.video_id}`;
  const indexRemoteResult = async (item: GlobalSearchResult) => {
    if (!item.remote || !item.remote_peer_type || !item.remote_peer_id || !item.remote_topic_id) {
      setRemoteIndexError("Este resultado no pertenece a un topic indexable.");
      return;
    }
    setRemoteIndexError("");
    setRemoteIndexing(item.id);
    try {
      const response = await fetch("/api/v1/search/remote/index", { method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ peer_type: item.remote_peer_type, peer_id: item.remote_peer_id, topic_id: item.remote_topic_id, message_id: item.remote_message_id }) });
      const data = await response.json() as { group_id?: string; topic_id?: string; message_id?: string; message?: string };
      if (!response.ok || !data.group_id || !data.topic_id) throw new Error(data.message || "No se pudo indexar el topic.");
      if (data.group_id && data.topic_id && item.remote_message_id) {
        sessionStorage.setItem(`tgstrm.import-review:${data.group_id}`, JSON.stringify({
          message_ids_by_topic: { [data.topic_id]: data.message_id ? [data.message_id] : [] },
          match_queries_by_topic: { [data.topic_id]: item.title.trim() || item.detail?.trim() || "" },
        }));
        sessionStorage.removeItem(`tgstrm.import-media-types:${data.group_id}`);
      }
      setGlobalSearchOpen(false);
      navigate(`/groups/${data.group_id}/import/review/${data.topic_id}`);
    } catch (error) {
      setRemoteIndexError(error instanceof Error ? error.message : "No se pudo indexar el topic.");
    } finally {
      setRemoteIndexing(undefined);
    }
  };
  const prepareLocalResult = (item: GlobalSearchResult) => {
    if (!item.group_id || !item.topic_id || !item.id) return;
    sessionStorage.setItem(`tgstrm.import-review:${item.group_id}`, JSON.stringify({
      message_ids_by_topic: { [item.topic_id]: [item.id] },
      match_queries_by_topic: { [item.topic_id]: item.title.trim() || item.detail?.trim() || "" },
    }));
    sessionStorage.removeItem(`tgstrm.import-media-types:${item.group_id}`);
    setGlobalSearchOpen(false);
    navigate(`/groups/${item.group_id}/import/review/${item.topic_id}`);
  };
  const searchKeyDown = (event: ReactKeyboardEvent<HTMLInputElement>) => {
    if (!globalSearchResults.length) return;
    if (event.key === "ArrowDown") { event.preventDefault(); setGlobalSearchSelected(value => (value + 1) % globalSearchResults.length); }
    if (event.key === "ArrowUp") { event.preventDefault(); setGlobalSearchSelected(value => (value - 1 + globalSearchResults.length) % globalSearchResults.length); }
    if (event.key === "Enter") { event.preventDefault(); const selected = globalSearchResults[globalSearchSelected]; if (selected.remote) { void indexRemoteResult(selected); } else if (selected.importable) { prepareLocalResult(selected); } else { setGlobalSearchOpen(false); navigate(searchResultHref(selected)); } }
  };

  const logoutAdmin = async () => { await fetch("/api/v1/admin/logout",{method:"POST",credentials:"same-origin"}); navigate("/admin/login",{replace:true}); };

  useEffect(() => {
    if (mode !== "activity") return;
    const focus = new URLSearchParams(location.search).get("focus");
    if (!focus || !activity.length) return;
    const animationFrame = window.requestAnimationFrame(() => document.getElementById(focus)?.scrollIntoView({ block: "center" }));
    return () => window.cancelAnimationFrame(animationFrame);
  }, [activity, location.search, mode]);

  useEffect(() => {
    if (!groupId || !topicId) return;
    const watch = () => fetch(`/api/v1/groups/${groupId}/topics/${topicId}/watch`, {
      method: "POST",
      credentials: "same-origin",
    }).catch(() => undefined);
    void watch();
    const timer = window.setInterval(watch, 20_000);
    return () => window.clearInterval(timer);
  }, [groupId, topicId]);

  useEffect(() => {
    if (mode === "manage") {
      sessionStorage.setItem("tgstrm.workspace-mode", "manage");
      setWorkspaceMode("manage");
    }
  }, [mode]);

  const selectWorkspaceMode = (nextMode: "catalog" | "manage") => {
    sessionStorage.setItem("tgstrm.workspace-mode", nextMode);
    setWorkspaceMode(nextMode);
  };

  const logoutTelegram = async () => {
    setLogoutBusy(true);
    setLogoutError("");
    try {
      const response = await fetch("/api/v1/telegram/logout", { method: "POST", credentials: "same-origin" });
      const data = await response.json() as { message?: string };
      if (!response.ok) throw new Error(data.message || "No se pudo cerrar la sesión.");
      sessionStorage.removeItem("tgstrm.workspace-mode");
      navigate("/login", { replace: true });
    } catch (error) {
      setLogoutError((error as Error).message);
      setLogoutBusy(false);
    }
  };

  useEffect(() => {
    if (!logoutOpen) return;
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !logoutBusy) setLogoutOpen(false);
    };
    window.addEventListener("keydown", closeOnEscape);
    return () => window.removeEventListener("keydown", closeOnEscape);
  }, [logoutOpen, logoutBusy]);

  const openEpisodeResolution = async (message: TopicMessage) => {
    if (!groupId || !topicId) return;
    setResolutionTarget(message); setResolutionInfo(undefined); setEpisodeOptions([]); setResolutionError(""); setResolutionState("loading");
    try {
      const response = await fetch(`/api/v1/groups/${groupId}/topics/${topicId}/videos/${message.id}/episode-resolution`, { credentials: "same-origin" });
      const data = await response.json() as EpisodeResolution & { message?: string };
      if (!response.ok) throw new Error(data.message || "No se pudo preparar el episodio.");
      setResolutionInfo(data);
      const season = data.saved.exists ? data.saved.season : data.suggested.detected ? data.suggested.season : 1;
      setResolutionSeason(season); setResolutionEpisode(data.saved.exists ? data.saved.episode : data.suggested.episode || 0); setResolutionState("idle");
    } catch (error) { setResolutionError((error as Error).message); setResolutionState("idle"); }
  };

  const loadSeasonEpisodes = async () => {
    if (!groupId || !topicId || !resolutionTarget) return;
    setResolutionState("episodes"); setResolutionError("");
    try {
      const response = await fetch(`/api/v1/groups/${groupId}/topics/${topicId}/videos/${resolutionTarget.id}/episodes?season=${resolutionSeason}`, { credentials: "same-origin" });
      const data = await response.json() as { episodes?: EpisodeOption[]; message?: string };
      if (!response.ok) throw new Error(data.message || "No se pudo cargar la temporada.");
      setEpisodeOptions(data.episodes || []);
      if (!(data.episodes || []).some((episode) => episode.number === resolutionEpisode)) setResolutionEpisode(0);
    } catch (error) { setResolutionError((error as Error).message); }
    finally { setResolutionState("idle"); }
  };

  const saveEpisodeResolution = async () => {
    if (!groupId || !topicId || !resolutionTarget || resolutionEpisode < 1) return;
    setResolutionState("saving"); setResolutionError("");
    try {
      const response = await fetch(`/api/v1/groups/${groupId}/topics/${topicId}/videos/${resolutionTarget.id}/episode-resolution`, {
        method: "PUT", credentials: "same-origin", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ season: resolutionSeason, episode: resolutionEpisode }),
      });
      const data = await response.json() as { relative_path?: string; message?: string };
      if (!response.ok) throw new Error(data.message || "No se pudo guardar el episodio.");
      setMessages((current) => current.map((message) => message.id === resolutionTarget.id ? { ...message, export_error: undefined } : message));
      setExportState(data.relative_path ? `STRM creado · ${data.relative_path}` : "Episodio asignado y STRM creado.");
      setResolutionTarget(undefined); setResolutionInfo(undefined); setEpisodeOptions([]);
    } catch (error) { setResolutionError((error as Error).message); }
    finally { setResolutionState("idle"); }
  };

  const updateScrollIndicator = () => {
    const frame = frameRef.current;
    if (!frame) return;

    const scrollable = frame.scrollHeight > frame.clientHeight + 1;
    const trackHeight = Math.max(0, frame.clientHeight - 28);
    const height = scrollable
      ? Math.max(34, (frame.clientHeight / frame.scrollHeight) * trackHeight)
      : trackHeight;
    const available = Math.max(0, trackHeight - height);
    const top = scrollable
      ? (frame.scrollTop / (frame.scrollHeight - frame.clientHeight)) *
        available
      : 0;

    setScrollIndicator({ visible: scrollable, height, top });

    if (window.matchMedia("(max-width: 720px)").matches) {
      if (!mobileCondensed && frame.scrollTop > 36) {
        setMobileCondensed(true);
      } else if (mobileCondensed && frame.scrollTop < 8) {
        setMobileCondensed(false);
      }
    }
  };

  const scrollFromThumbPosition = (thumbTop: number) => {
    const frame = frameRef.current;
    const edge = scrollEdgeRef.current;
    if (!frame || !edge) return;

    const available = Math.max(0, edge.clientHeight - scrollIndicator.height);
    const boundedTop = Math.min(Math.max(thumbTop, 0), available);
    frame.scrollTop = available
      ? (boundedTop / available) * (frame.scrollHeight - frame.clientHeight)
      : 0;
  };

  const handleTrackPointerDown = (event: ReactPointerEvent<HTMLSpanElement>) => {
    if (event.button !== 0) return;
    const edge = scrollEdgeRef.current;
    if (!edge) return;

    event.preventDefault();
    window.getSelection()?.removeAllRanges();
    event.currentTarget.setPointerCapture(event.pointerId);
    setDraggingScroll(true);
    dragOffsetRef.current = scrollIndicator.height / 2;
    scrollFromThumbPosition(
      event.clientY - edge.getBoundingClientRect().top - dragOffsetRef.current,
    );
  };

  const handleThumbPointerDown = (event: ReactPointerEvent<HTMLSpanElement>) => {
    if (event.button !== 0) return;
    event.preventDefault();
    event.stopPropagation();
    const edge = scrollEdgeRef.current;
    if (!edge) return;

    window.getSelection()?.removeAllRanges();
    edge.setPointerCapture(event.pointerId);
    setDraggingScroll(true);
    dragOffsetRef.current =
      event.clientY - edge.getBoundingClientRect().top - scrollIndicator.top;
  };

  const handleTrackPointerMove = (event: ReactPointerEvent<HTMLSpanElement>) => {
    if (dragOffsetRef.current === undefined) return;
    const edge = scrollEdgeRef.current;
    if (!edge) return;

    event.preventDefault();
    scrollFromThumbPosition(
      event.clientY - edge.getBoundingClientRect().top - dragOffsetRef.current,
    );
  };

  const finishScrollDrag = (event: ReactPointerEvent<HTMLSpanElement>) => {
    dragOffsetRef.current = undefined;
    setDraggingScroll(false);
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
  };

  useEffect(() => {
    let active = true;
    fetch("/api/v1/profile", { credentials: "same-origin" })
      .then(async (response) => {
        if (!response.ok) throw new Error();
        return response.json() as Promise<Profile>;
      })
      .then((data) => active && setProfile(data))
      .catch(() => active && setFailed(true));
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    let stopped = false;
    const checkAuthorization = () => {
      fetch("/api/v1/setup/status", { credentials: "same-origin" })
        .then((response) => response.ok ? response.json() as Promise<{ telegram: string }> : Promise.reject())
        .then((status) => {
          if (!stopped && status.telegram !== "authorized" && status.telegram !== "connected") navigate("/login", { replace: true });
        })
        .catch(() => undefined);
    };
    const timer = window.setInterval(checkAuthorization, 15000);
    const onVisible = () => { if (document.visibilityState === "visible") checkAuthorization(); };
    window.addEventListener("focus", checkAuthorization);
    document.addEventListener("visibilitychange", onVisible);
    return () => { stopped = true; window.clearInterval(timer); window.removeEventListener("focus", checkAuthorization); document.removeEventListener("visibilitychange", onVisible); };
  }, [navigate]);

  useEffect(() => {
    const frame = frameRef.current;
    if (!frame) return;

    const animationFrame = window.requestAnimationFrame(updateScrollIndicator);
    const observer = new ResizeObserver(updateScrollIndicator);
    observer.observe(frame);

    return () => {
      window.cancelAnimationFrame(animationFrame);
      observer.disconnect();
    };
  }, [groups, topics, messages, groupQuery, profile, groupDetail, topicDetail]);

  useEffect(() => {
    if (!groupId || !topicId) {
      setTopicDetail(undefined);
      setMessages([]);
      setMessageState("ready");
      setMissingEpisodes([]);
      return;
    }
    topicCatalogCompleteRef.current = false;
    const controller = new AbortController();
    setMessageState("loading");
    fetch(`/api/v1/groups/${encodeURIComponent(groupId)}/topics/${encodeURIComponent(topicId)}`, {
      credentials: "same-origin",
      signal: controller.signal,
    })
      .then(async (response) => {
        if (response.status === 404) { setMessageState("missing"); return undefined; }
        if (!response.ok) throw new Error();
        return response.json() as Promise<MessageResponse>;
      })
      .then((data) => {
        if (!data) return;
        setTopicDetail(data.topic);
        setMessages(data.messages);
        setNextCursor(data.next_cursor);
        setHasMoreMessages(data.has_more);
        topicCatalogCompleteRef.current = !data.has_more;
        setMessageState(data.sync);
      })
      .catch((error: unknown) => {
        if ((error as { name?: string }).name !== "AbortError") setMessageState("stale");
      });
    return () => controller.abort();
  }, [groupId, topicId]);

  useEffect(() => {
    if (!groupId || !topicId) return;
    const controller = new AbortController();
    fetch(`/api/v1/groups/${encodeURIComponent(groupId)}/topics/${encodeURIComponent(topicId)}/episode-gaps?refresh=1`, { credentials: "same-origin", signal: controller.signal })
      .then((response) => response.ok ? response.json() as Promise<{ missing: MissingEpisode[] }> : Promise.reject())
      .then((data) => setMissingEpisodes(data.missing || []))
      .catch((error: unknown) => { if ((error as { name?: string }).name !== "AbortError") setMissingEpisodes([]); });
    return () => controller.abort();
  }, [groupId, topicId]);

  useEffect(() => {
    if (!videoId) { setVideoDetail(undefined); setVideoState("ready"); return; }
    const controller = new AbortController();
    setVideoState("loading");
    fetch(`/api/v1/videos/${encodeURIComponent(videoId)}`, { credentials: "same-origin", signal: controller.signal })
      .then(async (response) => {
        if (response.status === 404) { setVideoState("missing"); return undefined; }
        if (!response.ok) throw new Error();
        return response.json() as Promise<VideoResponse>;
      })
      .then((data) => { if (data) { setVideoDetail(data); setVideoState("ready"); } })
      .catch((error: unknown) => { if ((error as { name?: string }).name !== "AbortError") setVideoState("missing"); });
    return () => controller.abort();
  }, [videoId]);

  const loadMoreMessages = () => {
    if (!groupId || !topicId || !nextCursor || loadingMore) return;
    setLoadingMore(true);
    fetch(`/api/v1/groups/${encodeURIComponent(groupId)}/topics/${encodeURIComponent(topicId)}?before=${nextCursor}`, { credentials: "same-origin" })
      .then((response) => response.ok ? response.json() as Promise<MessageResponse> : Promise.reject())
      .then((data) => {
        setMessages((current) => [...current, ...data.messages.filter((incoming) => !current.some((item) => item.id === incoming.id))]);
        setNextCursor(data.next_cursor);
        setHasMoreMessages(data.has_more);
        topicCatalogCompleteRef.current = !data.has_more;
      })
      .catch(() => undefined)
      .finally(() => setLoadingMore(false));
  };

  useEffect(() => {
    if (!topicId || !hasMoreMessages || !nextCursor || loadingMore || messageState === "loading") return;
    const timer = window.setTimeout(loadMoreMessages, 1250);
    return () => window.clearTimeout(timer);
  }, [topicId, hasMoreMessages, nextCursor, loadingMore, messageState]);

  useEffect(() => {
    fetch("/api/v1/groups", { credentials: "same-origin" })
      .then((response) => (response.ok ? response.json() : Promise.reject()))
      .then((data: { groups: Group[] }) => {
        setGroups(data.groups);
      })
      .catch(() => undefined);
  }, []);

  useEffect(() => {
    setSelectingTopics(false);
    setSelectingVideos(false);
    setSelectedTopicIds(new Set());
    setSelectedMessageIds(new Set());
    setExportState("");
  }, [groupId, topicId, activeMode]);

  useEffect(() => {
    if (mode !== "import" || !groupId) return;
    const routeTopicIDs = (topicIds || "").split(",").filter((id) => /^[1-9][0-9]*$/.test(id));
    const topicIDs = [...new Set(routeTopicIDs)];
    if (!topicIDs.length) { navigate(`/groups/${groupId}/manage`, { replace: true }); return; }
    setImportReviewState("loading");
    let stopped = false;
    let preparationFinished = false;
    const loadPreview = async (prepare: boolean) => {
      try {
        const mediaTypes = JSON.parse(sessionStorage.getItem(`tgstrm.import-media-types:${groupId}`) || "{}") as Record<string, "tv" | "movie">;
        const selection = JSON.parse(sessionStorage.getItem(`tgstrm.import-review:${groupId}`) || "{}") as ImportReviewSelection;
        const response = await fetch(`/api/v1/groups/${encodeURIComponent(groupId)}/imports/preview`, {
          method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ topic_ids: topicIDs, prepare, media_types: mediaTypes, match_queries: selection.match_queries_by_topic || {} }),
        });
        const data = await response.json() as { items?: ImportPreview[]; image_base_url?: string; message?: string };
        if (!response.ok || !data.items) throw new Error(data.message || "No se pudo preparar la revisión.");
        const incomingItems = data.items;
        if (stopped) return;
        setImportPreview((current) => prepare ? incomingItems : incomingItems.map((incoming) => {
          const previous = current.find((item) => item.topic_id === incoming.topic_id);
          return previous ? { ...previous, video_count: incoming.video_count, preparation_status: incoming.preparation_status, preparation_error: incoming.preparation_error } : incoming;
        }));
        if (prepare) {
          setImportChoices(Object.fromEntries(incomingItems.map((item) => [item.topic_id, item.suggested_id || item.candidates[0]?.id || 0])));
          setImportQueries(Object.fromEntries(incomingItems.map((item) => [item.topic_id, item.source_title])));
          const nextTypes = Object.fromEntries(incomingItems.map((item) => [item.topic_id, item.media_type || mediaTypes[item.topic_id] || "tv"])) as Record<string, "tv" | "movie">;
          setImportMediaTypes(nextTypes);
          sessionStorage.setItem(`tgstrm.import-media-types:${groupId}`, JSON.stringify(nextTypes));
          setImportDestinations((current) => Object.fromEntries(incomingItems.map((item) => {
            const existing = current[item.topic_id] || "";
            const destinations = item.destinations || [];
            const movie = nextTypes[item.topic_id] === "movie";
            const defaultDestination = movie ? destinations.find((destination) => destination.library_kind === "movie_collection" || /pel[ií]culas/i.test(destination.title))?.id || "" : existing;
            return [item.topic_id, existing || defaultDestination];
          })));
        }
        if (data.image_base_url) setTMDBImageBase(data.image_base_url);
        setImportReviewState("ready");
        preparationFinished = incomingItems.every((item) => item.preparation_status === "ready" || item.preparation_status === "failed");
      } catch (error) {
        if (!stopped) { setExportState((error as Error).message); setImportReviewState("error"); }
      }
    };
    void loadPreview(true);
    const interval = window.setInterval(() => { if (!preparationFinished) void loadPreview(false); }, 1000);
    return () => { stopped = true; window.clearInterval(interval); };
  }, [mode, groupId, topicIds, navigate]);

  useEffect(() => {
    const topicIDs = mode === "import" ? importPreview.map((item) => item.topic_id) : topicId ? [topicId] : [];
    if (!groupId || !topicIDs.length) return;
    let stopped = false;
    Promise.all(topicIDs.map(async (id) => {
      const response = await fetch(`/api/v1/groups/${encodeURIComponent(groupId)}/topics/${encodeURIComponent(id)}/import`, { credentials: "same-origin" });
      return [id, response.ok ? await response.json() as ImportStatus : { status: "idle" } as ImportStatus] as const;
    })).then((entries) => { if (!stopped) setImportStatuses((current) => ({ ...current, ...Object.fromEntries(entries) })); });
    return () => { stopped = true; };
  }, [groupId, topicId, mode, importPreview, importRefreshNonce]);

  useEffect(() => {
    const currentTopicID = topicId;
    const current = currentTopicID ? importStatuses[currentTopicID] : undefined;
    if (!currentTopicID || !current || (current.status !== "completed" && current.status !== "failed")) return;
    const finishedTopicID = currentTopicID;
    const timer = window.setTimeout(() => setImportStatuses((statuses) => { const next = { ...statuses }; delete next[finishedTopicID]; return next; }), 6000);
    return () => window.clearTimeout(timer);
  }, [topicId, importStatuses]);

  useEffect(() => {
    if (!exportState.startsWith("Importaciones iniciadas")) return;
    const timer = window.setTimeout(() => setExportState(""), 6000);
    return () => window.clearTimeout(timer);
  }, [exportState]);

  useEffect(() => {
    setTMDBInitialScanFinished(topicId ? sessionStorage.getItem(`tgstrm.tmdb-initial:${topicId}`) === "complete" : false);
    setTMDBScanProgress(undefined);
  }, [groupId, topicId]);

  useEffect(() => {
    if (!groupId || !topicId) {
      setTMDBMatches([]);
      return;
    }
    const controller = new AbortController();
    fetch(`/api/v1/groups/${encodeURIComponent(groupId)}/topics/${encodeURIComponent(topicId)}/tmdb-match`, {
      credentials: "same-origin", signal: controller.signal,
    })
      .then((response) => response.ok ? response.json() as Promise<{ matches: Array<Omit<TMDBMatch, "match_id" | "id"> & { id: string }>; image_base_url?: string }> : Promise.reject())
      .then((data) => {
        setTMDBMatches(data.matches.map((match) => ({ ...match, match_id: match.id, id: match.tmdb_id })));
        if (data.image_base_url) setTMDBImageBase(data.image_base_url);
      })
      .catch((error: unknown) => { if ((error as { name?: string }).name !== "AbortError") setTMDBMatches([]); });
    return () => controller.abort();
  }, [groupId, topicId]);

  useEffect(() => {
    if (activeMode !== "manage" || !groupId || !topicId) return;
    let stopped = false;
    void fetch(`/api/v1/groups/${encodeURIComponent(groupId)}/topics/${encodeURIComponent(topicId)}/tmdb-scan`, { method: "POST", credentials: "same-origin" });
    const refresh = async () => {
      try {
        const [statusResponse, matchesResponse] = await Promise.all([
          fetch(`/api/v1/groups/${encodeURIComponent(groupId)}/topics/${encodeURIComponent(topicId)}/tmdb-scan`, { credentials: "same-origin" }),
          fetch(`/api/v1/groups/${encodeURIComponent(groupId)}/topics/${encodeURIComponent(topicId)}/tmdb-match`, { credentials: "same-origin" }),
        ]);
        if (statusResponse.ok) {
          const progress = await statusResponse.json() as { running: boolean; total: number; matched: number; reviewed: number };
          setTMDBScanProgress(progress);
          if (!progress.running && progress.reviewed >= progress.total) {
            setTMDBInitialScanFinished(true);
            sessionStorage.setItem(`tgstrm.tmdb-initial:${topicId}`, "complete");
          }
        }
        if (matchesResponse.ok) {
          const data = await matchesResponse.json() as { matches: Array<Omit<TMDBMatch, "match_id" | "id"> & { id: string }>; image_base_url?: string };
          setTMDBMatches(data.matches.map((match) => ({ ...match, match_id: match.id, id: match.tmdb_id })));
          if (data.image_base_url) setTMDBImageBase(data.image_base_url);
        }
      } catch { /* the next poll retries */ }
      if (!stopped) window.setTimeout(refresh, 2500);
    };
    void refresh();
    return () => { stopped = true; };
  }, [activeMode, groupId, topicId, messages.length]);

  useEffect(() => {
    if (!groupId || topicId) { setTopicArtwork({}); return; }
    let stopped = false;
    void fetch(`/api/v1/groups/${encodeURIComponent(groupId)}/topics/artwork`, { method: "POST", credentials: "same-origin" });
    const refresh = async () => {
      try {
        const response = await fetch(`/api/v1/groups/${encodeURIComponent(groupId)}/topics/artwork`, { credentials: "same-origin" });
        if (!response.ok) return;
        const data = await response.json() as { artwork: TopicArtwork[]; running: boolean; backdrop_base_url?: string; poster_base_url?: string };
        if (stopped) return;
        setTopicArtwork(Object.fromEntries(data.artwork.map((item) => [item.topic_id, item])));
        if (data.backdrop_base_url) setTMDBBackdropBase(data.backdrop_base_url);
        if (data.poster_base_url) setTMDBImageBase(data.poster_base_url);
        if (data.running) window.setTimeout(refresh, 2200);
      } catch { /* artwork remains optional while TMDB reconnects */ }
    };
    window.setTimeout(refresh, 250);
    return () => { stopped = true; };
  }, [groupId, topicId, topics.length]);

  const generateSTRM = async (topicIDs: string[], messageIDs?: string[]) => {
    if (!groupId || !topicIDs.length) return;
    setExportState("Generando STRM…");
    let written = 0;
    let requested = 0;
    let unresolved = 0;
    try {
      for (const currentTopicID of topicIDs) {
        const response = await fetch(`/api/v1/groups/${encodeURIComponent(groupId)}/topics/${encodeURIComponent(currentTopicID)}/exports`, {
          method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ message_ids: messageIDs || [], public_base_url: window.location.origin }),
        });
        const data = await response.json() as { written?: number; unresolved?: number; requested?: number; message?: string };
        if (!response.ok) throw new Error(data.message || "No se pudo generar la biblioteca.");
        written += data.written || 0; requested += data.requested || 0; unresolved += data.unresolved || 0;
      }
      setExportState(`${written} de ${requested} STRM escritos correctamente${unresolved ? ` · ${unresolved} sin episodio identificable` : ""}`);
    } catch (error) { setExportState((error as Error).message); }
  };

  const openImportReview = (topicIDs: string[], messageIDsByTopic: Record<string, string[]> = {}) => {
    if (!groupId || !topicIDs.length) return;
    const orderedTopicIDs = [...new Set(topicIDs)].sort((left, right) => Number(left) - Number(right));
    sessionStorage.setItem(`tgstrm.import-review:${groupId}`, JSON.stringify({ message_ids_by_topic: messageIDsByTopic }));
    sessionStorage.removeItem(`tgstrm.import-media-types:${groupId}`);
    navigate(`/groups/${groupId}/import/review/${orderedTopicIDs.join(",")}`);
  };

  const confirmReviewedImports = async () => {
    if (!groupId || !importPreview.length) return;
    const stored = JSON.parse(sessionStorage.getItem(`tgstrm.import-review:${groupId}`) || "{}") as ImportReviewSelection;
    setImportReviewState("sending");
    try {
      for (const item of importPreview) {
        const tmdbID = importChoices[item.topic_id];
        if (!tmdbID) throw new Error(`Selecciona una coincidencia para ${item.source_title}.`);
        const response = await fetch(`/api/v1/groups/${encodeURIComponent(groupId)}/topics/${encodeURIComponent(item.topic_id)}/import`, {
          method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ message_ids: stored.message_ids_by_topic?.[item.topic_id] || [], tmdb_id: tmdbID, media_type: importMediaTypes[item.topic_id] || item.media_type || "tv", destination_topic_id: importDestinations[item.topic_id] || "" }),
        });
        const data = await response.json() as { requested?: number; destination_title?: string; message?: string };
        if (!response.ok) throw new Error(data.message || `No se pudo preparar ${item.source_title}.`);
        setImportStatuses((current) => ({ ...current, [item.topic_id]: { status: "pending", requested: data.requested || item.video_count, completed: 0, destination_title: data.destination_title } }));
      }
      setExportState("Importaciones iniciadas. Puedes salir de esta vista; el progreso queda guardado.");
      setImportReviewState("ready");
      setImportRefreshNonce((value) => value + 1);
    } catch (error) {
      setExportState((error as Error).message);
      setImportReviewState("ready");
    }
  };

  const selectedMessagesForImport = (topicID: string): string[] => {
    if (!groupId) return [];
    try {
    const stored = JSON.parse(sessionStorage.getItem(`tgstrm.import-review:${groupId}`) || "{}") as ImportReviewSelection;
      return stored.message_ids_by_topic?.[topicID] || [];
    } catch {
      return [];
    }
  };

  const importItemCanConfirm = (item: ImportPreview) => {
    const selectedMessages = selectedMessagesForImport(item.topic_id);
    return item.preparation_status === "ready" || selectedMessages.length > 0;
  };

  const changeImportMediaType = async (topicID: string, mediaType: "tv" | "movie") => {
    if (!groupId) return;
    const next = { ...importMediaTypes, [topicID]: mediaType };
    setImportMediaTypes(next);
    sessionStorage.setItem(`tgstrm.import-media-types:${groupId}`, JSON.stringify(next));
    setImportChoices((choices) => ({ ...choices, [topicID]: 0 }));
    try {
      const response = await fetch(`/api/v1/groups/${encodeURIComponent(groupId)}/imports/preview`, {
        method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ topic_ids: [topicID], prepare: true, media_types: next, match_queries: { [topicID]: importQueries[topicID] || "" } }),
      });
      const data = await response.json() as { items?: ImportPreview[]; message?: string };
      if (!response.ok || !data.items?.length) throw new Error(data.message || "No se pudo cambiar el tipo de contenido.");
      const incoming = data.items[0];
      setImportPreview((items) => items.map((item) => item.topic_id === topicID ? incoming : item));
      const first = incoming.suggested_id || incoming.candidates[0]?.id || 0;
      setImportChoices((choices) => ({ ...choices, [topicID]: first }));
      if (mediaType === "movie") {
        const defaultDestination = (incoming.destinations || []).find((destination) => destination.library_kind === "movie_collection" || /pel[ií]culas/i.test(destination.title));
        if (defaultDestination) setImportDestinations((destinations) => ({ ...destinations, [topicID]: destinations[topicID] || defaultDestination.id }));
      }
    } catch (error) {
      setExportState((error as Error).message);
    }
  };

  const searchImportMatch = async (topicID: string) => {
    const query = importQueries[topicID]?.trim();
    if (!query || query.length < 2) return;
    setImportSearching(topicID);
    try {
      const response = await fetch(`/api/v1/tmdb/search?media_type=${importMediaTypes[topicID] || "tv"}&query=${encodeURIComponent(query)}`, { credentials: "same-origin" });
      const data = await response.json() as { results?: TMDBResult[]; message?: string };
      if (!response.ok || !data.results) throw new Error(data.message || "TMDB no pudo completar la búsqueda.");
      setImportPreview((items) => items.map((item) => item.topic_id === topicID ? { ...item, candidates: data.results || [] } : item));
      setImportChoices((choices) => ({ ...choices, [topicID]: 0 }));
    } catch (error) {
      setExportState((error as Error).message);
    } finally {
      setImportSearching(undefined);
    }
  };

  const startGroupVideoScan = async () => {
    if (!groupId || groupVideoScan?.status === "running") return;
    const response = await fetch(`/api/v1/groups/${encodeURIComponent(groupId)}/video-scan`, {
      method: "POST",
      credentials: "same-origin",
    });
    if (!response.ok) return;
    setGroupVideoScan((current) => ({
      status: "running",
      total_topics: current?.total_topics || topics.length,
      completed_topics: 0,
      video_count: current?.video_count || topics.reduce((sum, topic) => sum + topic.video_count, 0),
      total_bytes: current?.total_bytes || 0,
      failed_topics: 0,
    }));
    setScanPollNonce((value) => value + 1);
  };

  const searchTopicMatch = async (requestedQuery?: string) => {
    const search = (requestedQuery ?? topicMatchQuery).trim();
    if (search.length < 2) return;
    setTopicMatchState("searching");
    setTopicMatchError("");
    try {
      const response = await fetch(`/api/v1/tmdb/search?media_type=tv&query=${encodeURIComponent(search)}`, { credentials: "same-origin" });
      const data = await response.json() as { results?: TMDBResult[]; message?: string };
      if (!response.ok || !data.results) throw new Error(data.message || "TMDB no pudo completar la búsqueda.");
      setTopicMatchResults(data.results);
      if (!data.results.length) setTopicMatchError("No encontramos series con ese título. Prueba con el título original o añade el año.");
    } catch (error) {
      setTopicMatchError((error as Error).message);
      setTopicMatchResults([]);
    } finally { setTopicMatchState("idle"); }
  };

  const openTopicMatch = () => {
    const initial = (topicDetail?.title || "").replace(/\s*\[tmdbid-\d+\]\s*$/i, "").trim();
    setTopicMatchOpen(true);
    setTopicMatchQuery(initial);
    setTopicMatchResults([]);
    setTopicMatchError("");
    if (initial) void searchTopicMatch(initial);
  };

  const confirmTopicMatch = async (candidate: TMDBResult) => {
    if (!groupId || !topicId) return;
    setTopicMatchState("saving");
    setTopicMatchError("");
    try {
      const response = await fetch(`/api/v1/groups/${encodeURIComponent(groupId)}/topics/${encodeURIComponent(topicId)}/tmdb-match`, {
        method: "PUT", credentials: "same-origin", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ media_type: "tv", tmdb_id: candidate.id }),
      });
      const data = await response.json() as { id?: string; match?: TMDBResult; message?: string };
      if (!response.ok || !data.match || !data.id) throw new Error(data.message || "No se pudo guardar la coincidencia.");
      setTMDBMatches((current) => [{ ...data.match!, match_id: data.id!, media_type: "tv", tmdb_id: data.match!.id, message_id: "" }, ...current.filter((item) => item.message_id)]);
      setExportState(`TMDB asociado a ${data.match.title}${data.match.release_date ? ` (${data.match.release_date.slice(0, 4)})` : ""}.`);
      setTopicMatchOpen(false);
    } catch (error) { setTopicMatchError((error as Error).message); }
    finally { setTopicMatchState("idle"); }
  };

  const searchMovieMatch = async (requestedQuery?: string) => {
    const search = (requestedQuery ?? movieMatchQuery).trim();
    if (search.length < 2) return;
    setMovieMatchState("searching");
    setMovieMatchError("");
    try {
      const response = await fetch(`/api/v1/tmdb/search?media_type=movie&query=${encodeURIComponent(search)}`, { credentials: "same-origin" });
      const data = await response.json() as { results?: TMDBResult[]; message?: string };
      if (!response.ok || !data.results) throw new Error(data.message || "TMDB no pudo completar la búsqueda.");
      setMovieMatchResults(data.results);
      if (!data.results.length) setMovieMatchError("No encontramos películas con ese título. Prueba con el título original o añade el año.");
    } catch (error) {
      setMovieMatchError((error as Error).message);
      setMovieMatchResults([]);
    } finally { setMovieMatchState("idle"); }
  };

  const openMovieMatch = (message: TopicMessage) => {
    const initial = (message.body.split("\n")[0] || message.file_name.replace(/\.[^.]+$/, "")).trim();
    setMovieMatchTarget(message);
    setMovieMatchQuery(initial);
    setMovieMatchResults([]);
    setMovieMatchError("");
    if (initial) void searchMovieMatch(initial);
  };

  const confirmMovieMatch = async (candidate: TMDBResult) => {
    if (!groupId || !topicId || !movieMatchTarget) return;
    setMovieMatchState("saving");
    setMovieMatchError("");
    try {
      const response = await fetch(`/api/v1/groups/${groupId}/topics/${topicId}/tmdb-match`, {
        method: "PUT", credentials: "same-origin", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ media_type: "movie", tmdb_id: candidate.id, message_id: movieMatchTarget.id }),
      });
      const data = await response.json() as { id?: string; match?: TMDBResult; message?: string };
      if (!response.ok || !data.id || !data.match) throw new Error(data.message || "No se pudo guardar la película.");
      const targetID = movieMatchTarget.id;
      setTMDBMatches((current) => [
        { ...data.match!, id: data.match!.id, match_id: data.id!, media_type: "movie", tmdb_id: data.match!.id, message_id: targetID },
        ...current.filter((match) => match.message_id !== targetID),
      ]);
      setMessages((current) => current.map((message) => message.id === targetID ? { ...message, export_error: undefined } : message));
      setMovieMatchTarget(undefined);
      setExportState(`${data.match.title}${data.match.release_date ? ` (${data.match.release_date.slice(0, 4)})` : ""} se asociará y generará automáticamente.`);
    } catch (error) {
      setMovieMatchError((error as Error).message);
    } finally { setMovieMatchState("idle"); }
  };

  useEffect(() => {
    if (!groupId) { setGroupVideoScan(undefined); return; }
    let stopped = false;
    let timer: number | undefined;
    const refresh = async () => {
      try {
        const response = await fetch(`/api/v1/groups/${encodeURIComponent(groupId)}/video-scan`, { credentials: "same-origin" });
        if (!response.ok) return;
        const status = await response.json() as GroupVideoScan;
        if (stopped) return;
        setGroupVideoScan(status);
        if (status.status === "running") timer = window.setTimeout(refresh, 1500);
      } catch { /* WebSocket keeps the catalog useful while status reconnects. */ }
    };
    void refresh();
    return () => { stopped = true; window.clearTimeout(timer); };
  }, [groupId, scanPollNonce]);

  useEffect(() => {
    let stopped = false;
    let socket: WebSocket | undefined;
    let reconnectTimer: number | undefined;
    let retry = 0;
    let refreshTimer: number | undefined;
	let activityTimer: number | undefined;

	const reloadOperationalState = () => {
	  window.clearTimeout(activityTimer);
	  activityTimer = window.setTimeout(() => {
		if (!stopped) setActivityRefreshNonce((value) => value + 1);
	  }, 350);
	};

	const reloadGroups = () => {
	  window.clearTimeout(refreshTimer);
	  refreshTimer = window.setTimeout(() => {
		fetch("/api/v1/groups", { credentials: "same-origin" })
		  .then((response) => response.ok ? response.json() as Promise<{ groups: Group[] }> : Promise.reject())
		  .then((data) => { if (!stopped) setGroups(data.groups); })
		  .catch(() => undefined);
	  }, 80);
	};

    const reloadCachedTopics = () => {
	  if (!groupId) return;
      window.clearTimeout(refreshTimer);
      refreshTimer = window.setTimeout(() => {
        fetch(`/api/v1/groups/${encodeURIComponent(groupId)}/topics?refresh=0`, {
          credentials: "same-origin",
        })
          .then((response) =>
            response.ok ? (response.json() as Promise<TopicResponse>) : Promise.reject(),
          )
          .then((data) => {
            if (stopped) return;
            setGroupDetail(data.group);
            setTopics(data.topics);
            setCatalogState(data.sync);
          })
          .catch(() => undefined);
      }, 80);
    };

    const reloadCachedTopicArtwork = () => {
	  if (!groupId || topicId) return;
	  fetch(`/api/v1/groups/${encodeURIComponent(groupId)}/topics/artwork`, { credentials: "same-origin" })
		.then((response) => response.ok ? response.json() as Promise<{ artwork: TopicArtwork[]; backdrop_base_url?: string; poster_base_url?: string }> : Promise.reject())
		.then((data) => {
		  if (stopped) return;
		  setTopicArtwork(Object.fromEntries(data.artwork.map((item) => [item.topic_id, item])));
		  if (data.backdrop_base_url) setTMDBBackdropBase(data.backdrop_base_url);
		  if (data.poster_base_url) setTMDBImageBase(data.poster_base_url);
		})
		.catch(() => undefined);
	};

    const reloadCurrentVideos = () => {
	  if (!topicId || !groupId) return;
	  const currentGroupID = groupId;
	  const currentTopicID = topicId;
	  window.clearTimeout(refreshTimer);
	  refreshTimer = window.setTimeout(() => {
		fetch(`/api/v1/groups/${encodeURIComponent(currentGroupID)}/topics/${encodeURIComponent(currentTopicID)}?refresh=0`, { credentials: "same-origin" })
		  .then((response) => response.ok ? response.json() as Promise<MessageResponse> : Promise.reject())
		  .then((data) => {
			if (stopped) return;
			setTopicDetail(data.topic);
			setMessages((current) => [...data.messages, ...current.filter((stored) => !data.messages.some((fresh) => fresh.id === stored.id))]);
			if (!topicCatalogCompleteRef.current) {
			  setNextCursor(data.next_cursor);
			  setHasMoreMessages(data.has_more);
			  topicCatalogCompleteRef.current = !data.has_more;
			}
			setMessageState(data.sync);
			fetch(`/api/v1/groups/${encodeURIComponent(currentGroupID)}/topics/${encodeURIComponent(currentTopicID)}/episode-gaps?refresh=0`, { credentials: "same-origin" })
			  .then((response) => response.ok ? response.json() as Promise<{ missing: MissingEpisode[] }> : Promise.reject())
			  .then((gaps) => { if (!stopped) setMissingEpisodes(gaps.missing || []); })
			  .catch(() => undefined);
		  }).catch(() => undefined);
	  }, 80);
    };

    const connect = () => {
      const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
      socket = new WebSocket(`${protocol}//${window.location.host}/api/v1/events`);
      socket.onopen = () => {
        retry = 0;
		topicId ? reloadCurrentVideos() : groupId ? reloadCachedTopics() : reloadGroups();
      };
      socket.onmessage = (message) => {
        try {
          const event = JSON.parse(message.data) as { type: string; aggregate_id: string };
          if (event.type === "topics.snapshot" && event.aggregate_id === groupId) {
            reloadCachedTopics();
			reloadCachedTopicArtwork();
          }
		  if (event.type === "videos.snapshot" && event.aggregate_id === topicId) {
			reloadCurrentVideos();
		  }
		  if (event.type === "groups.snapshot" && !groupId) { reloadGroups(); }
		  if (event.type === "catalog.import.progress") { setImportRefreshNonce((value) => value + 1); }
		  if (event.type === "mirror.progress") { window.dispatchEvent(new CustomEvent("tgstrm:mirror-progress")); }
		  reloadOperationalState();
        } catch { /* Ignore malformed server events and keep the stream alive. */ }
      };
      socket.onclose = () => {
        if (stopped) return;
        retry += 1;
        reconnectTimer = window.setTimeout(connect, Math.min(1000 * 2 ** retry, 15000));
      };
    };
    connect();
    return () => {
      stopped = true;
      window.clearTimeout(reconnectTimer);
      window.clearTimeout(refreshTimer);
	  window.clearTimeout(activityTimer);
      socket?.close();
    };
  }, [groupId, topicId]);

  useEffect(() => {
    if (!groupId) {
      setGroupDetail(undefined);
      setTopics([]);
      setCatalogState("ready");
      return;
    }
    const controller = new AbortController();
    setCatalogState("loading");
    setGroupQuery("");
    fetch(`/api/v1/groups/${encodeURIComponent(groupId)}/topics`, {
      credentials: "same-origin",
      signal: controller.signal,
    })
      .then(async (response) => {
        if (response.status === 404) {
          setCatalogState("missing");
          return undefined;
        }
        if (!response.ok) throw new Error();
        return response.json() as Promise<TopicResponse>;
      })
      .then((data) => {
        if (!data) return;
        setGroupDetail(data.group);
        setTopics(data.topics);
        setCatalogState(data.sync);
      })
      .catch((error: unknown) => {
        if ((error as { name?: string }).name !== "AbortError") {
          setCatalogState("stale");
        }
      });
    return () => controller.abort();
  }, [groupId]);

  if (failed) return <Navigate to="/login" replace />;
  if (!profile)
    return <main className="dashboard-loading">Preparando tu inicio…</main>;

  const initials =
    `${profile.first_name[0] || "T"}${profile.last_name[0] || ""}`.toUpperCase();
  const query = groupQuery.trim().toLocaleLowerCase();
  const visibleGroups = query
    ? groups.filter((group) =>
        `${group.title} ${group.username || ""}`
          .toLocaleLowerCase()
          .includes(query),
      )
    : groups;
  const visibleTopics = query
    ? topics.filter((topic) =>
        topic.title.toLocaleLowerCase().includes(query),
      )
    : topics;
  const isPrimaryGroup = groups.some((group) => group.active && group.id === groupId);
  const focusedActivityKey = new URLSearchParams(location.search).get("focus") || "";
  const notificationItems = notificationActivity.filter((item) => {
    const needsAttention = item.kind === "review" || item.status === "failed" || item.status === "error" || item.status === "missing";
    if (notificationLevel === "essential") return needsAttention;
    if (notificationLevel === "balanced") return needsAttention || ((item.kind === "import" || item.kind === "topic_deleted" || item.kind === "episode_replaced" || item.kind === "backup") && item.status === "completed");
    return true;
  });
  const unreadNotifications = notificationItems.filter((item) => !notificationsReadAt || new Date(item.occurred_at) > new Date(notificationsReadAt));
  const openNotifications = () => {
    const nextReadAt = new Date().toISOString();
    setNotificationsOpen((open) => !open);
    setNotificationsReadAt(nextReadAt);
    localStorage.setItem("tgstrm.notifications-read-at", nextReadAt);
  };
  const clearHistory = async (scope: "activity" | "notifications") => {
    const response = await fetch("/api/v1/activity/clear", {
      method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ scope }),
    });
    if (!response.ok) return;
    if (scope === "activity") {
      setActivity([]);
      setExpandedActivity(new Set());
    } else {
      setNotificationActivity([]);
      setNotificationsOpen(false);
    }
  };
  const clearActivityFocus = (event: ReactPointerEvent<HTMLElement>) => {
    if (!focusedActivityKey) return;
    const target = event.target as Element;
    if (target.closest(".activity-item")?.id === focusedActivityKey) return;
    navigate("/activity", { replace: true, preventScrollReset: true });
  };
  const retryJob = async (job: JobItem) => {
    setRetryingJob(job.id);
    setJobRetryError("");
    try {
      const response = await fetch(`/api/v1/jobs/${job.kind}/${job.id}/retry`, { method: "POST", credentials: "same-origin" });
      const data = await response.json() as { message?: string };
      if (!response.ok) throw new Error(data.message || "No se pudo reintentar el trabajo.");
      setActivityRefreshNonce((current) => current + 1);
    } catch (error) { setJobRetryError((error as Error).message); } finally { setRetryingJob(undefined); }
  };
  const visibleMessages = messages.filter((message) => !query || `${message.body} ${message.file_name} ${message.media_type || ""}`.toLocaleLowerCase().includes(query));
  const visibleMissingEpisodes = missingEpisodes.filter((episode) => !query || `${episode.title} S${String(episode.season).padStart(2, "0")}E${String(episode.episode).padStart(2, "0")}`.toLocaleLowerCase().includes(query));
  const orderedTopicItems = topicDetail?.library_kind === "series"
    ? [
        ...visibleMessages.map((message) => ({ kind: "video" as const, season: message.season_number || 999, episode: message.episode_number || message.telegram_id, message })),
        ...visibleMissingEpisodes.map((episode) => ({ kind: "missing" as const, season: episode.season, episode: episode.episode, missing: episode })),
      ].sort((left, right) => left.season - right.season || left.episode - right.episode || (left.kind === "video" ? -1 : 1))
    : visibleMessages.map((message) => ({ kind: "video" as const, season: 0, episode: 0, message }));

  return (
    <div
      className={`dashboard-page${dark ? " dashboard-dark" : ""}${mobileCondensed ? " mobile-condensed" : ""}${activeMode === "manage" ? " management-mode" : ""}`}
    >
      <header className="dashboard-header">
        <Brand />
        <nav className="top-nav" aria-label="Navegación principal">
          <Link className={activeMode === "catalog" ? "active" : ""} to="/home" onClick={() => selectWorkspaceMode("catalog")}>
            Inicio
          </Link>
          <Link className={activeMode === "mirror" ? "active" : ""} to="/mirror">Espejo</Link>
          <Link className={mode === "activity" ? "active" : ""} to="/activity">Actividad</Link>
        </nav>
        <div className="dashboard-account-area" ref={notificationAreaRef}>
          <div className="utility-pill" aria-label="Herramientas">
            <button
              aria-label="Buscar"
              title="Buscar en el catálogo"
              aria-expanded={globalSearchOpen}
              onClick={() => { setNotificationsOpen(false); setAccountOpen(false); setGlobalSearchOpen(true); }}
            >
              <Search size={18} />
            </button>
            <button
              className={`notification-trigger${notificationsOpen ? " active" : ""}`}
              aria-label="Notificaciones"
              title="Notificaciones"
              aria-expanded={notificationsOpen}
              onClick={openNotifications}
            >
              <Bell size={18} />
              {unreadNotifications.length > 0 && <span className="notification-badge">{Math.min(unreadNotifications.length, 99)}</span>}
            </button>
            <button aria-label="Ayuda" title="Estado de esta etapa">
              <CircleHelp size={18} />
            </button>
          </div>
          {notificationsOpen && <section className="notification-popover" aria-label="Notificaciones recientes">
            <header><div><p className="eyebrow">ACTIVIDAD</p><strong>Notificaciones</strong></div><span className="notification-header-actions">{notificationItems.length > 0 && <button type="button" onClick={() => void clearHistory("notifications")}><Trash2 size={12} /> Limpiar</button>}<Link to="/activity" onClick={() => setNotificationsOpen(false)}>Ver todas</Link></span></header>
            <div>{notificationItems.slice(0, 6).map((item) => <Link key={`${item.kind}-${item.id}`} to={`/activity?focus=${item.kind}-${item.id}`} onClick={() => setNotificationsOpen(false)}>
              <span className={`activity-mark ${item.status}`}>{item.kind === "import" ? <Send size={14} /> : item.kind === "mirror" ? <RefreshCw size={14} /> : item.kind === "review" ? <CircleHelp size={14} /> : item.kind === "topic_deleted" ? <Trash2 size={14} /> : item.kind === "episode_replaced" ? <RotateCcw size={14} /> : <FileVideo size={14} />}</span>
              <span><strong>{item.title}</strong><small>{item.detail}</small></span>
            </Link>)}</div>
            {!notificationItems.length && <p className="notification-empty"><CheckCircle2 size={17} /> No hay avisos para este nivel.</p>}
          </section>}
          <div className="profile-menu-wrap" ref={accountAreaRef}><button type="button" className="profile-pill" onClick={()=>setAccountOpen(value=>!value)} aria-expanded={accountOpen} aria-label="Cuenta y seguridad">
            {profile.avatar_url ? (
              <img src={profile.avatar_url} alt="" />
            ) : (
              <span className="profile-initials">{initials}</span>
            )}
            <span className="profile-copy">
              <strong>{profile.display_name}</strong>
              <small>{profile.phone}</small>
            </span>
            <ChevronDown size={14}/>
          </button>{accountOpen&&<section className="profile-popover"><header><span>{adminUsername.slice(0,2).toUpperCase()}</span><div><strong>{adminUsername}</strong><small>Cuenta administradora</small></div></header><div className="profile-telegram-row">{profile.avatar_url?<img src={profile.avatar_url} alt=""/>:<span>{initials}</span>}<div><strong>{profile.display_name}</strong><small>Telegram · {profile.phone}</small></div></div><Link to="/settings" onClick={()=>setAccountOpen(false)}><ShieldCheck size={15}/> Cuenta y seguridad</Link><button type="button" onClick={()=>void logoutAdmin()}><LogOut size={15}/> Cerrar sesión web</button></section>}</div>
        </div>
      </header>

      {globalSearchOpen && <div className="global-search-layer" role="presentation" onPointerDown={(event) => { if (event.target === event.currentTarget) setGlobalSearchOpen(false); }}>
        <section className="global-search-dialog" role="dialog" aria-modal="true" aria-labelledby="global-search-title">
          <header>
            <Search size={19}/>
            <input ref={globalSearchInputRef} value={globalSearchQuery} onChange={event => setGlobalSearchQuery(event.target.value)} onKeyDown={searchKeyDown} placeholder="Buscar archivos, series, películas o topics…" aria-label="Buscar en el catálogo" autoComplete="off" spellCheck={false}/>
            {globalSearchQuery && <button type="button" onClick={() => { setGlobalSearchQuery(""); globalSearchInputRef.current?.focus(); }} aria-label="Limpiar búsqueda"><X size={16}/></button>}
            <button type="button" className="global-search-close" onClick={() => setGlobalSearchOpen(false)} aria-label="Cerrar búsqueda"><X size={17}/></button>
          </header>
          <div className="global-search-body">
            {globalSearchState === "loading" ? <div className="global-search-loading"><LoaderCircle className="spin" size={20}/> Buscando en tu catálogo…</div> : globalSearchState === "error" ? <div className="global-search-loading error">No se pudo consultar el catálogo.</div> : globalSearchState === "ready" && globalSearchResults.length ? <div className="global-search-results" role="listbox" aria-label="Resultados de búsqueda">{globalSearchResults.map((item,index)=>item.remote ? <div key={`${item.kind}-${item.id}`} className={`global-search-remote-result${index===globalSearchSelected?" selected":""}`} role="option" aria-selected={index===globalSearchSelected} onMouseEnter={()=>setGlobalSearchSelected(index)}>
              <span className="global-result-art"><Radio size={18}/></span><span className="global-result-copy"><strong>{item.title}</strong><small>{item.group_title || "Telegram"}</small>{item.detail&&<em>{item.detail}</em>}</span><span className="global-result-meta"><b className="remote">Telegram</b>{item.remote_topic_id ? <button type="button" className="global-remote-index" onClick={()=>void indexRemoteResult(item)} disabled={remoteIndexing===item.id}>{remoteIndexing===item.id ? <LoaderCircle className="spin" size={11}/> : <ScanSearch size={11}/>} {remoteIndexing===item.id ? "Preparando" : "Preparar envío"}</button> : <small>Sin topic</small>}</span>
            </div> : <div key={`${item.kind}-${item.id}`} className={`global-search-local-row${index===globalSearchSelected?" selected":""}`} role="option" aria-selected={index===globalSearchSelected} onMouseEnter={()=>setGlobalSearchSelected(index)}>
              <span className={`global-result-art${item.poster_path?" has-poster":""}`}>{item.poster_path?<img src={`${tmdbImageBase}${item.poster_path}`} alt=""/>:item.kind==="group"?<MessageCircle size={18}/>:item.kind==="topic"?<FolderOpen size={18}/>:<FileVideo size={18}/>}</span>
              <span className="global-result-copy"><strong>{item.title}</strong><small>{item.kind==="file"?`${item.topic_title} · ${item.group_title}`:item.kind==="topic"?`${item.group_title} · Topic`:"Grupo de Telegram"}</small>{item.matched&&<em><Sparkles size={11}/> {item.official_title}{item.year?` (${item.year})`:""}</em>}</span>
              <span className="global-result-meta">{item.kind==="file"&&item.byte_size?<small>{formatBytes(item.byte_size)}</small>:null}<b className={item.matched?"matched":""}>{item.matched?"TMDB":item.kind==="file"?"Sin asociar":item.kind}</b>{item.exported&&<small>STRM</small>}{item.importable&&<button type="button" className="global-remote-index" onClick={()=>prepareLocalResult(item)}><ScanSearch size={11}/> Preparar envío</button>}</span>
              {item.importable ? <button type="button" className="global-search-row-open" onClick={()=>{ setGlobalSearchOpen(false); navigate(searchResultHref(item)); }} aria-label="Abrir archivo en el reproductor"><ArrowUpRight size={13}/></button> : <button type="button" className="global-search-row-open" onClick={()=>{ setGlobalSearchOpen(false); navigate(searchResultHref(item)); }} aria-label="Abrir resultado"><ArrowUpRight size={13}/></button>}
            </div>)}</div> : globalSearchState === "ready" && globalSearchQuery.trim().length>=2 ? <div className="global-search-empty"><span><Search size={20}/></span><strong>Sin resultados</strong><p>No encontramos “{globalSearchQuery.trim()}” en el catálogo ni en los resultados accesibles de Telegram.</p><small>Prueba con el nombre del archivo o con el pie de archivo.</small></div> : <div className="global-search-empty">
              <span><Sparkles size={20}/></span>
              <strong id="global-search-title">Busca en todo tu catálogo</strong>
              <p>Escribe el nombre de un archivo, película, serie, episodio o topic.</p>
              <small>Los resultados locales aparecerán primero, sin depender de Telegram ni de TMDB.</small>
            </div>}
            {remoteIndexError && <p className="global-search-inline-error" role="alert">{remoteIndexError}</p>}
          </div>
          <footer><span><kbd>↑</kbd><kbd>↓</kbd> Navegar</span><span><kbd>Enter</kbd> Abrir</span><span><kbd>Esc</kbd> Cerrar</span></footer>
        </section>
      </div>}

      <section className="dashboard-intro">
        <div>
          <p className="eyebrow">{activeMode === "mirror" ? "SINCRONIZACIÓN" : activeMode === "settings" ? "ADMINISTRACIÓN" : activeMode === "activity" ? "TRAZABILIDAD" : activeMode === "manage" ? "GESTIÓN DE TOPICS" : videoId ? "VÍDEO DE TELEGRAM" : topicId ? "TOPIC DE TELEGRAM" : groupId ? "GRUPO DE TELEGRAM" : "CENTRO DE CONTROL"}</p>
          <h1>{activeMode === "mirror" ? mode === "mirror-manage" ? "Gestionar espejo" : "Espejo" : activeMode === "settings" ? "Configuración" : activeMode === "activity" ? "Actividad" : mode === "import" ? "Revisar coincidencias" : activeMode === "manage" ? groupDetail?.title || "Cargando grupo…" : videoId ? videoDetail?.video.caption || videoDetail?.video.file_name || "Cargando vídeo…" : topicId ? topicDetail?.title || "Cargando topic…" : groupId ? groupDetail?.title || "Cargando grupo…" : `Hola, ${profile.first_name}.`}</h1>
          <p>
            {activeMode === "mirror"
              ? mode === "mirror-manage" ? "Vincula topics externos con el catálogo principal." : "Supervisa los topics que se mantienen sincronizados en tiempo real."
              : activeMode === "settings"
              ? "Conexiones y destinos que utiliza tu biblioteca STRM."
              : activeMode === "activity"
              ? "Importaciones, archivos STRM y elementos que necesitan revisión."
              : mode === "import"
              ? "Confirma cómo se organizará cada serie antes de enviarla al grupo principal."
              : activeMode === "manage"
              ? "Selecciona los topics que quieres preparar para la biblioteca."
              : videoId
              ? videoDetail ? `${videoDetail.group.title} · ${videoDetail.topic.title}` : "Preparando la ficha del archivo"
              : topicId
              ? groupDetail?.title || "Historial del topic"
              : groupId
              ? groupDetail?.forum
                ? `${topics.length} topics disponibles en este foro.`
                : "Este grupo no utiliza topics."
              : "Tu servidor está listo para construir el catálogo de Telegram."}
          </p>
        </div>
      </section>

      <aside className="pill-rail" aria-label="Accesos rápidos">
        <div className="rail-group appearance-group">
          <button
            className={!dark ? "active" : ""}
            onClick={() => setDark(false)}
            aria-label="Tema claro"
            title="Tema claro"
          >
            <Sun size={17} />
          </button>
          <button
            className={dark ? "active" : ""}
            onClick={() => setDark(true)}
            aria-label="Tema oscuro"
            title="Tema oscuro"
          >
            <Moon size={17} />
          </button>
        </div>
        <div className="rail-group main-group">
          {activeMode === "mirror" ? <>
            <Link className={mode === "mirror" ? "active" : ""} to="/mirror" aria-label="Resumen del espejo" title="Resumen del espejo"><Network size={18}/></Link>
            <Link className={mode === "mirror-manage" ? "active" : ""} to="/mirror/manage" aria-label="Gestionar espejo" title="Gestionar espejo"><Link2 size={18}/></Link>
          </> : <>
          <Link
            className={activeMode === "catalog" ? "active" : ""}
            to={topicId ? `/groups/${groupId}/topics/${topicId}` : groupId ? `/groups/${groupId}` : "/home"}
            replace
            onClick={() => selectWorkspaceMode("catalog")}
            aria-label="Vista normal"
            title="Vista normal"
          >
            <Home size={18} />
          </Link>
          <Link className={activeMode === "manage" ? "active" : ""} to={topicId ? `/groups/${groupId}/topics/${topicId}/manage` : groupId ? `/groups/${groupId}/manage` : "/home"} replace onClick={() => selectWorkspaceMode("manage")} aria-label={topicId ? "Gestionar archivos" : groupId ? "Gestionar topics" : "Gestión"} title={topicId ? "Gestionar archivos" : groupId ? "Gestionar topics" : "Gestión"}><CheckSquare2 size={18} /></Link>
          </>}
          <Link className={`mobile-settings-link${mode === "settings" ? " active" : ""}`} to="/settings" aria-label="Ajustes" title="Ajustes"><Settings size={18} /></Link>
          <button type="button" className="mobile-logout-link" onClick={() => { setLogoutError(""); setLogoutOpen(true); }} aria-label="Cerrar sesión" title="Cerrar sesión de Telegram"><LogOut className="logout-icon" size={18} /></button>
        </div>
        <div className="rail-group bottom-group">
          <Link className={mode === "settings" ? "active" : ""} to="/settings" aria-label="Ajustes" title="Ajustes">
            <Settings size={18} />
          </Link>
          <button type="button" onClick={() => { setLogoutError(""); setLogoutOpen(true); }} aria-label="Cerrar sesión" title="Cerrar sesión de Telegram">
            <LogOut className="logout-icon" size={18} />
          </button>
        </div>
      </aside>

      <div
        className={`dashboard-frame-wrap${draggingScroll ? " is-scroll-dragging" : ""}`}
      >
      <main
        ref={frameRef}
        className={`dashboard-frame${videoId ? " video-frame" : ""}`}
        onScroll={updateScrollIndicator}
      >
        {activeMode === "mirror" ? <MirrorPanel manage={mode === "mirror-manage"} /> : activeMode === "settings" ? (
          <><AdminAccountPanel /><MediaSettingsPanel /></>
        ) : activeMode === "activity" ? (
          <section className="activity-view" aria-label="Actividad del sistema" onPointerDown={clearActivityFocus}>
            <section className="jobs-panel" aria-label="Trabajos en segundo plano"><header><div><strong>Trabajos</strong><small>Procesos persistentes que siguen funcionando aunque cierres la web.</small></div><span>{jobs.some((job) => job.status === "failed") ? `${jobs.filter((job) => job.status === "failed").length} requieren atención` : `${jobs.length} en curso`}</span></header>{jobRetryError && <p className="jobs-error" role="alert">{jobRetryError}</p>}{jobs.length ? <div className="jobs-list">{jobs.map((job) => <article className={`job-item ${job.status}`} key={`${job.kind}-${job.id}`}><Link to={job.href || "/activity"}><span className="job-mark">{job.kind === "import" ? <Send size={15} /> : job.kind === "mirror" ? <RefreshCw size={15} /> : job.kind === "scan" ? <Radio size={15} /> : <FileVideo size={15} />}</span><span><strong>{job.title}</strong><small>{job.detail}</small></span><em>{job.status === "failed" || job.status === "stale" ? "Revisar" : job.status === "pending" ? "En cola" : "En curso"}</em></Link>{job.kind === "import" && job.status === "failed" && <button type="button" onClick={() => void retryJob(job)} disabled={retryingJob === job.id}>{retryingJob === job.id ? <LoaderCircle className="spin" size={13} /> : <RotateCcw size={13} />} Reintentar</button>}</article>)}</div> : <div className="jobs-empty"><CircleCheck size={16} /> No hay trabajos pendientes ni incidencias activas.</div>}{recentJobs.length > 0 && <div className="jobs-recent"><small>Últimos completados</small>{recentJobs.map((job) => <Link to={job.href || "/activity"} key={`recent-${job.id}`}><CircleCheck size={13} /><span><strong>{job.title}</strong> · {job.detail}</span></Link>)}</div>}</section>
            <header className="activity-toolbar"><div><strong>Historial del sistema</strong><small>Los registros operativos se conservan de forma segura.</small></div>{activity.length > 0 && <button type="button" onClick={() => void clearHistory("activity")}><Trash2 size={14} /> Limpiar actividad</button>}</header>
            <div className="activity-list">
            {activity.length ? activity.map((item) => {
              const activityKey = `${item.kind}-${item.id}`;
              const expanded = expandedActivity.has(activityKey);
              return <article id={activityKey} className={`activity-item${focusedActivityKey === activityKey ? " focused" : ""}`} key={activityKey}>
              <span className={`activity-mark ${item.status}`}>{item.kind === "import" ? <Send size={16} /> : item.kind === "mirror" ? <RefreshCw size={16} /> : item.kind === "review" ? <CircleHelp size={16} /> : item.kind === "topic_deleted" ? <Trash2 size={16} /> : item.kind === "episode_replaced" ? <RotateCcw size={16} /> : <FileVideo size={16} />}</span>
              <div><strong>{item.title}</strong><p>{item.detail}</p><small>{new Intl.DateTimeFormat("es", { dateStyle: "medium", timeStyle: "short" }).format(new Date(item.occurred_at))}</small></div>
              <span className={`activity-status ${item.status}`}>{item.status === "completed" || item.status === "written" ? "Completado" : item.status === "pending" ? "Pendiente" : item.status}</span>
              {!!item.children?.length && <button className={`activity-expand${expanded ? " expanded" : ""}`} type="button" aria-expanded={expanded} onClick={() => setExpandedActivity((current) => { const next = new Set(current); expanded ? next.delete(activityKey) : next.add(activityKey); return next; })}><ChevronDown size={15} /> <span>{expanded ? "Ocultar" : "Detalles"}</span></button>}
              {item.href && <Link className="activity-open" to={item.href}>Abrir</Link>}
              {expanded && !!item.children?.length && <div className="activity-children">{item.children.map((child) => <div className={`activity-child ${child.status}`} key={child.id}><span className={`activity-child-dot ${child.status}`} /><span title={child.detail}>{child.detail}</span><small>{new Intl.DateTimeFormat("es", { dateStyle: "short", timeStyle: "short" }).format(new Date(child.occurred_at))}</small></div>)}</div>}
            </article>}) : <div className="topics-empty"><CheckCircle2 size={22} /><h2>Sin actividad registrada</h2><p>Los eventos nuevos aparecerán aquí automáticamente.</p></div>}
            </div>
          </section>
        ) : <><section className="topics-toolbar">
          <div>
            <p className="eyebrow">TELEGRAM</p>
            <h2>{mode === "import" ? "Revisión TMDB" : videoId ? "Reproductor" : topicId ? "Vídeos" : groupId ? "Topics" : "Tus grupos"}</h2>
          </div>
          {groupId && !videoId && <button className="catalog-summary" type="button" aria-label="Resumen del catálogo">
            <span className="catalog-summary-main"><FileVideo size={14} /><strong>{topics.reduce((sum, topic) => sum + topic.video_count, 0).toLocaleString("es")} vídeos</strong></span>
            <span className="catalog-summary-extra">· {formatBytes(topics.reduce((sum, topic) => sum + (topic.total_bytes || 0), 0) || groupVideoScan?.total_bytes || 0)}</span>
            <span className="catalog-summary-detail">
              {topics.length.toLocaleString("es")} topics · {topics.filter((topic) => topic.video_count === 0).length.toLocaleString("es")} sin vídeos cargados
              {groupVideoScan?.status === "running" ? ` · Escaneando ${groupVideoScan.completed_topics}/${groupVideoScan.total_topics}` : ""}
            </span>
          </button>}
          <div className="topics-toolbar-actions">
          {!videoId && <label className="group-search">
            <Search size={17} />
            <span className="sr-only">{topicId ? "Buscar vídeos" : groupId ? "Buscar topics" : "Buscar grupos"}</span>
            <input
              value={groupQuery}
              onChange={(event) => setGroupQuery(event.target.value)}
              placeholder={topicId ? "Buscar vídeos" : groupId ? "Buscar topics" : "Buscar grupos"}
            />
          </label>}
          </div>
        </section>
        {activeMode === "manage" && mode !== "import" && groupId && (
          <section className="selection-bar" aria-label="Selección de topics">
            <div className="media-kind" aria-label="Destino de biblioteca">
              <span>Destino automático</span>
            </div>
            <span />
            {!topicId && <button className="scan-action" disabled={groupVideoScan?.status === "running"} onClick={startGroupVideoScan} aria-label="Escanear vídeos" title="Escanear vídeos">
              <ScanSearch size={14} /> <span className="mobile-action-label">{groupVideoScan?.status === "running" ? `${groupVideoScan.completed_topics}/${groupVideoScan.total_topics}` : "Escanear vídeos"}</span>
            </button>}
            {!topicId ? selectingTopics ? <>
              <strong>{selectedTopicIds.size} seleccionados</strong>
              <button onClick={() => setSelectedTopicIds(new Set(visibleTopics.map((topic) => topic.id)))} aria-label="Seleccionar todos" title="Todos"><CheckSquare2 size={14} /><span className="mobile-action-label">Todos</span></button>
              <button onClick={() => setSelectedTopicIds(new Set())} disabled={!selectedTopicIds.size} aria-label="Deseleccionar todos" title="Ninguno"><Square size={14} /><span className="mobile-action-label">Ninguno</span></button>
              <button onClick={() => { setSelectingTopics(false); setSelectedTopicIds(new Set()); }} aria-label="Cancelar selección" title="Cancelar"><X size={14} /> <span className="mobile-action-label">Cancelar</span></button>
              {!isPrimaryGroup && <button disabled={!selectedTopicIds.size} onClick={() => openImportReview([...selectedTopicIds])} aria-label="Enviar topics al catálogo" title="Enviar topics al catálogo"><Send size={14} /><span className="mobile-action-label">Enviar topics</span></button>}
              <button className="selection-primary" disabled={!selectedTopicIds.size} onClick={() => generateSTRM([...selectedTopicIds])} aria-label="Generar STRM" title="Generar STRM"><FileVideo size={14} /><span className="mobile-action-label">Generar STRM</span></button>
            </> : <button className="selection-primary" onClick={() => setSelectingTopics(true)} aria-label="Seleccionar topics" title="Seleccionar topics"><CheckSquare2 size={15} /> <span className="mobile-action-label">Seleccionar topics</span></button>
            : selectingVideos ? <>
              <strong>{selectedMessageIds.size} seleccionados</strong>
              <button onClick={() => setSelectedMessageIds(new Set(messages.map((message) => message.id)))} aria-label="Seleccionar todos" title="Todos"><CheckSquare2 size={14} /><span className="mobile-action-label">Todos</span></button>
              <button onClick={() => setSelectedMessageIds(new Set())} disabled={!selectedMessageIds.size} aria-label="Deseleccionar todos" title="Ninguno"><Square size={14} /><span className="mobile-action-label">Ninguno</span></button>
              <button onClick={() => { setSelectingVideos(false); setSelectedMessageIds(new Set()); }} aria-label="Cancelar selección" title="Cancelar"><X size={14} /> <span className="mobile-action-label">Cancelar</span></button>
              {!isPrimaryGroup && <button disabled={!selectedMessageIds.size} onClick={() => openImportReview([topicId], { [topicId]: [...selectedMessageIds] })} aria-label="Enviar al catálogo" title="Enviar al catálogo"><Send size={14} /> <span className="mobile-action-label">Enviar al catálogo</span></button>}
              <button className="selection-primary" disabled={!selectedMessageIds.size} onClick={() => generateSTRM([topicId], [...selectedMessageIds])} aria-label="Generar seleccionados" title="Generar seleccionados"><FileVideo size={14} /><span className="mobile-action-label">Generar seleccionados</span></button>
            </> : <>
              {isPrimaryGroup && topicDetail?.library_kind === "series" && <button onClick={openTopicMatch} aria-label="Revisar coincidencia TMDB" title="Revisar coincidencia TMDB"><ScanSearch size={15} /> <span className="mobile-action-label">Revisar TMDB</span></button>}
              <button onClick={() => setSelectingVideos(true)} aria-label="Seleccionar archivos" title="Seleccionar archivos"><CheckSquare2 size={15} /> <span className="mobile-action-label">Seleccionar archivos</span></button>
              {!isPrimaryGroup && <button onClick={() => openImportReview([topicId])} aria-label="Enviar topic al catálogo" title="Enviar topic al catálogo"><Send size={14} /> <span className="mobile-action-label">Enviar topic al catálogo</span></button>}
              <button className="selection-primary" onClick={() => generateSTRM([topicId])} aria-label="Generar topic completo" title="Generar topic completo"><FileVideo size={14} /><span className="mobile-action-label">Generar topic completo</span></button>
            </>}
          </section>
        )}
        {activeMode === "manage" && exportState && <div className="export-feedback" role="status">{exportState}</div>}
        {activeMode === "manage" && topicId && importStatuses[topicId] && importStatuses[topicId].status !== "idle" && <div className={`export-feedback import-${importStatuses[topicId].status}`} role="status">
          {importStatuses[topicId].status === "running" || importStatuses[topicId].status === "pending" ? <><LoaderCircle className="spin" size={14} /> Enviando {importStatuses[topicId].completed || 0} de {importStatuses[topicId].requested || 0} a {importStatuses[topicId].destination_title || "tu catálogo"}</> : importStatuses[topicId].status === "completed" ? <><CheckCircle2 size={14} /> {importStatuses[topicId].completed || 0} archivos enviados correctamente a {importStatuses[topicId].destination_title}</> : <><CircleHelp size={14} /> Enviados {importStatuses[topicId].completed || 0} de {importStatuses[topicId].requested || 0} · {importStatuses[topicId].failed || 0} con error</>}
        </div>}
        {activeMode === "manage" && topicId && tmdbScanProgress?.running && !tmdbInitialScanFinished && tmdbScanProgress.reviewed < tmdbScanProgress.total && <div className="export-feedback" role="status"><LoaderCircle className="spin" size={14} /> Analizando con TMDB: {tmdbScanProgress.reviewed} de {tmdbScanProgress.total} · {tmdbScanProgress.matched} coincidencias seguras</div>}
        {mode === "import" ? (
          <section className="import-review" aria-label="Revisión de coincidencias TMDB">
            {importReviewState === "loading" ? <div className="topics-empty"><LoaderCircle className="spin" size={22} /><h2>Buscando coincidencias</h2><p>Consultando TMDB para preparar los topics seleccionados.</p></div> : <>
              <div className="import-review-heading"><div><p className="eyebrow">ANTES DE ENVIAR</p><h3>{importPreview.length} {importPreview.length === 1 ? "topic preparado" : "topics preparados"}</h3><p>Selecciona la ficha correcta, el tipo de contenido y el destino. El título y los datos de TMDB se aplicarán al enviarlo.</p></div><button type="button" className="review-confirm" disabled={importReviewState === "sending" || importPreview.some((item) => !importChoices[item.topic_id] || !importItemCanConfirm(item) || (item.video_count === 0 && selectedMessagesForImport(item.topic_id).length === 0))} onClick={confirmReviewedImports}>{importReviewState === "sending" ? <LoaderCircle className="spin" size={15} /> : <Send size={15} />} Confirmar y enviar</button></div>
              <div className="import-review-list">{importPreview.map((item) => <article className="import-review-item" key={item.topic_id}>
                <header><div><strong>{item.source_title}</strong><small>{selectedMessagesForImport(item.topic_id).length > 0 && item.preparation_status !== "ready" ? `${selectedMessagesForImport(item.topic_id).length} archivo seleccionado · el resto se indexa en segundo plano` : item.preparation_status === "running" || item.preparation_status === "idle" ? <><LoaderCircle className="spin" size={12} /> Cargando archivos desde Telegram…</> : item.preparation_status === "failed" ? "No se pudo cargar el historial" : `${item.video_count} vídeos encontrados`}</small></div><span>{item.preparation_status === "ready" && importChoices[item.topic_id] ? "Coincidencia seleccionada" : selectedMessagesForImport(item.topic_id).length > 0 ? "Listo para enviar" : item.preparation_status === "failed" ? "Error de preparación" : "Preparando topic"}</span></header>
                <div className="import-review-options">
                  <div className="import-media-toggle" role="group" aria-label={`Tipo de contenido de ${item.source_title}`}>
                    <button type="button" className={(importMediaTypes[item.topic_id] || item.media_type || "tv") === "tv" ? "selected" : ""} onClick={() => void changeImportMediaType(item.topic_id, "tv")}>Serie</button>
                    <button type="button" className={(importMediaTypes[item.topic_id] || item.media_type || "tv") === "movie" ? "selected" : ""} onClick={() => void changeImportMediaType(item.topic_id, "movie")}>Película</button>
                  </div>
                  <label className="import-destination"><span>Enviar a</span><select value={importDestinations[item.topic_id] || ""} onChange={(event) => setImportDestinations((current) => ({ ...current, [item.topic_id]: event.target.value }))}><option value="">Crear topic con el título de TMDB</option>{(item.destinations || []).filter((destination) => (importMediaTypes[item.topic_id] || item.media_type || "tv") === "movie" ? destination.library_kind === "movie_collection" || /pel[ií]culas/i.test(destination.title) : destination.library_kind !== "movie_collection").map((destination) => <option key={destination.id} value={destination.id}>{destination.title}{destination.library_kind === "movie_collection" ? " · Películas" : destination.library_kind === "series" ? " · Series" : ""}</option>)}</select></label>
                </div>
                <div className="import-match-search"><Search size={15} /><input value={importQueries[item.topic_id] || ""} onChange={(event) => setImportQueries((current) => ({ ...current, [item.topic_id]: event.target.value }))} onKeyDown={(event) => { if (event.key === "Enter") { event.preventDefault(); void searchImportMatch(item.topic_id); } }} aria-label={`Buscar otra coincidencia para ${item.source_title}`} /><button type="button" onClick={() => searchImportMatch(item.topic_id)} disabled={importSearching === item.topic_id}>{importSearching === item.topic_id ? <LoaderCircle className="spin" size={14} /> : "Buscar otra"}</button></div>
                <div className="import-candidates">{item.candidates.length ? item.candidates.map((candidate) => {
                  const selected = importChoices[item.topic_id] === candidate.id;
                  return <button type="button" key={candidate.id} className={selected ? "selected" : ""} onClick={() => setImportChoices((current) => ({ ...current, [item.topic_id]: candidate.id }))}>
                    <span className="import-poster">{candidate.poster_path ? <img src={`${tmdbImageBase}${candidate.poster_path}`} alt="" loading="lazy" /> : <FileVideo size={20} />}</span>
                    <span><strong>{candidate.title}</strong><small>{candidate.release_date?.slice(0, 4) || "Año desconocido"}{candidate.original_title && candidate.original_title !== candidate.title ? ` · ${candidate.original_title}` : ""}</small></span>
                    {selected && <CheckCircle2 size={17} />}
                  </button>;
                }) : <p className="import-no-match">TMDB no encontró candidatos. Ajusta el nombre del topic y vuelve a preparar la importación.</p>}</div>
                {importStatuses[item.topic_id] && importStatuses[item.topic_id].status !== "idle" && <div className="import-inline-progress"><span style={{ width: `${Math.min(100, ((importStatuses[item.topic_id].completed || 0) / Math.max(1, importStatuses[item.topic_id].requested || 1)) * 100)}%` }} /><small>{importStatuses[item.topic_id].status === "completed" ? "Completado" : `${importStatuses[item.topic_id].completed || 0} de ${importStatuses[item.topic_id].requested || 0}`}</small></div>}
              </article>)}</div>
            </>}
          </section>
        ) : videoId ? (
          videoState === "loading" ? (
            <section className="topics-empty"><div className="empty-icon"><FileVideo size={22} /></div><h2>Cargando información</h2></section>
          ) : videoState === "missing" || !videoDetail ? (
            <section className="topics-empty"><div className="empty-icon"><FileVideo size={22} /></div><h2>Este vídeo no está disponible</h2><Link className="back-link" to={`/groups/${groupId}/topics/${topicId}`}><ArrowLeft size={15} /> Volver al topic</Link></section>
          ) : (
            <section className="video-detail">
              <div
                className="video-main-grid"
                style={{ "--video-ratio": videoDetail.video.width && videoDetail.video.height ? videoDetail.video.width / videoDetail.video.height : 16 / 9 } as CSSProperties}
              >
                <div
                  className="video-player-shell"
                  style={{
                    aspectRatio: videoDetail.video.width && videoDetail.video.height ? `${videoDetail.video.width} / ${videoDetail.video.height}` : "16 / 9",
                  } as CSSProperties}
                >
                <video
                  key={`${videoId}-${playerAttempt}`}
                  className="video-player"
                  src={`/api/v1/videos/${encodeURIComponent(videoId)}/stream`}
                  controls
                  controlsList="nodownload"
                  preload="metadata"
                  playsInline
                  aria-label={videoDetail.video.file_name || "Vídeo de Telegram"}
                  onLoadedMetadata={() => setPlayerState("ready")}
                  onCanPlay={() => setPlayerState("ready")}
                  onPlaying={() => setPlayerState("ready")}
                  onTimeUpdate={() => setPlayerState("ready")}
                  onError={() => setPlayerState("error")}
                />
                {playerState === "loading" && (
                  <div className="video-player-status" aria-live="polite">
                    <LoaderCircle size={25} />
                    <span>Preparando vídeo</span>
                  </div>
                )}
                {playerState === "error" && (
                  <div className="video-player-status video-player-error" role="alert">
                    <FileVideo size={25} />
                    <strong>No se pudo reproducir este vídeo</strong>
                    <span>Comprueba la conexión con Telegram e inténtalo de nuevo.</span>
                    <button onClick={() => { setPlayerState("loading"); setPlayerAttempt((value) => value + 1); }}>
                      <RotateCcw size={15} /> Reintentar
                    </button>
                  </div>
                )}
                </div>
                <div className="video-detail-info">
                  <div className="video-info-cards">
                    <div className="video-detail-hero"><span><FileVideo size={24} /></span><div><p className="eyebrow">ARCHIVO ORIGINAL</p><h3>{videoDetail.video.file_name || "Vídeo sin nombre"}</h3><small>{videoDetail.video.mime_type || "Formato desconocido"}</small></div></div>
                    <article><small>Tamaño</small><strong>{formatBytes(videoDetail.video.byte_size)}</strong></article>
                    <article><small>Duración</small><strong>{formatDuration(videoDetail.video.duration_seconds)}</strong></article>
                    <article><small>Resolución</small><strong>{videoDetail.video.width && videoDetail.video.height ? `${videoDetail.video.width} × ${videoDetail.video.height}` : "Desconocida"}</strong></article>
                    <article><small>Publicado</small><strong>{new Intl.DateTimeFormat("es", { dateStyle: "medium", timeStyle: "short" }).format(new Date(videoDetail.video.sent_at))}</strong></article>
                  </div>
                  {videoDetail.video.caption && <article className="video-caption"><small>Pie del archivo</small><p>{videoDetail.video.caption}</p></article>}
                </div>
              </div>
            </section>
          )
        ) : topicId ? (
          messageState === "missing" ? (
            <section className="topics-empty">
              <div className="empty-icon"><FileVideo size={22} /></div>
              <h2>Este topic ya no está disponible</h2>
              <Link className="back-link" to={`/groups/${groupId}`}><ArrowLeft size={15} /> Volver al grupo</Link>
            </section>
          ) : messageState === "loading" || (topicDetail?.library_kind === "series" && hasMoreMessages) ? (
            <section className="topics-empty"><div className="empty-icon"><FileVideo size={22} /></div><h2>Cargando vídeos</h2><p>Consultando Telegram y preparando el catálogo del topic.</p></section>
          ) : messages.length || missingEpisodes.length ? (
            <section className="message-list" aria-label="Mensajes del topic">
              {orderedTopicItems.map((item) => {
                if (item.kind === "missing") {
                  const episode = item.missing;
                  return <article key={`missing-${episode.season}-${episode.episode}`} className="message-card missing-episode-card">
                    <span className="message-poster missing-episode-poster">{episode.still_path ? <img src={`https://image.tmdb.org/t/p/w300${episode.still_path}`} alt="" loading="lazy" /> : <FileVideo size={24} />}</span>
                    <div className="message-copy">
                      <p>S{String(episode.season).padStart(2, "0")}E{String(episode.episode).padStart(2, "0")} {episode.title || `Episodio ${episode.episode}`}</p>
                      <small>{episode.air_date ? `Emitido el ${new Intl.DateTimeFormat("es", { dateStyle: "medium" }).format(new Date(`${episode.air_date}T00:00:00`))}` : "Episodio publicado"}</small>
                      <span className="missing-episode-badge">Falta</span>
                    </div>
                  </article>;
                }
                const message = item.message;
                const selected = selectedMessageIds.has(message.id);
                const tmdbMatch = tmdbMatches.find((match) => match.message_id === message.id);
                const content = <>
                  <span className={`message-poster${tmdbMatch?.poster_path ? " has-poster" : ""}`}>{tmdbMatch?.poster_path ? <img src={`${tmdbImageBase}${tmdbMatch.poster_path}`} alt={`Póster de ${tmdbMatch.title}`} loading="lazy" /> : <FileVideo size={24} />}</span>
                  <div className="message-copy">
                    <p>{message.body || message.file_name || "Vídeo sin título"}</p>
                    {message.body && message.file_name && <span className="message-filename">{message.file_name}</span>}
                    <small>{new Intl.DateTimeFormat("es", { dateStyle: "medium", timeStyle: "short" }).format(new Date(message.sent_at))}{message.edited_at ? " · editado" : ""}</small>
                    {tmdbMatch && <span className="tmdb-match-badge" title={`TMDB #${tmdbMatch.tmdb_id}`}><CheckCircle2 size={12} /> {tmdbMatch.title}{tmdbMatch.release_date ? ` (${tmdbMatch.release_date.slice(0, 4)})` : ""}</span>}
                    {activeMode === "manage" && message.export_error && <button type="button" className="tmdb-match-badge export-error-badge resolution-trigger" title={message.export_error} onClick={() => topicDetail?.library_kind === "movie_collection" ? openMovieMatch(message) : void openEpisodeResolution(message)}><CircleHelp size={12} /> {topicDetail?.library_kind === "movie_collection" ? "Revisar película" : "Resolver episodio"}</button>}
                  </div>
                  {activeMode === "manage" && selectingVideos && <span className={`topic-check${selected ? " selected" : ""}`}>{selected ? <CheckSquare2 size={18} /> : <Square size={18} />}</span>}
                </>;
                return activeMode === "manage" && selectingVideos ? <button key={message.id} type="button" className={`message-card selectable${selected ? " selected" : ""}`} aria-pressed={selected} onClick={() => setSelectedMessageIds((current) => { const next = new Set(current); selected ? next.delete(message.id) : next.add(message.id); return next; })}>{content}</button> : activeMode === "manage" && message.export_error ? <article key={message.id} className="message-card unresolved-message-card">{content}</article> : <Link key={message.id} className="message-card" to={`/groups/${groupId}/topics/${topicId}/videos/${message.id}`}>{content}</Link>;
              })}
              {hasMoreMessages && !query && <div className="load-more" aria-live="polite"><LoaderCircle className="spin" size={15} /> Cargando el catálogo completo…</div>}
            </section>
          ) : (
            <section className="topics-empty"><div className="empty-icon"><FileVideo size={22} /></div><h2>{query ? "No encontramos ese vídeo" : "Este topic todavía no tiene vídeos"}</h2><Link className="back-link" to={`/groups/${groupId}`}><ArrowLeft size={15} /> Volver al grupo</Link></section>
          )
        ) : groupId ? (
          catalogState === "missing" ? (
            <section className="topics-empty">
              <div className="empty-icon"><FolderOpen size={22} /></div>
              <h2>Este grupo ya no está disponible</h2>
              <p>Puede que haya sido eliminado o que la cuenta haya salido de él.</p>
              <Link className="back-link" to="/home"><ArrowLeft size={15} /> Volver a tus grupos</Link>
            </section>
          ) : catalogState === "loading" ? (
            <section className="topics-empty">
              <div className="empty-icon"><MessageCircle size={22} /></div>
              <h2>Sincronizando topics</h2>
              <p>Estamos consultando Telegram y preparando el catálogo de este grupo.</p>
            </section>
          ) : visibleTopics.length ? (
            <section className="groups-grid topics-grid" aria-label="Topics del grupo">
              {visibleTopics.map((topic) => {
                const selected = selectedTopicIds.has(topic.id);
                const artwork = topicArtwork[topic.id];
                const content = <>
                  {artwork ? <span className={`topic-artwork ${artwork.kind}`}>
                    {artwork.kind === "show" && (artwork.backdrop_path || artwork.poster_paths[0]) ? <img src={`${artwork.backdrop_path ? tmdbBackdropBase : tmdbImageBase}${artwork.backdrop_path || artwork.poster_paths[0]}`} alt={`Portada de ${artwork.title || topic.title}`} loading="lazy" /> : artwork.poster_paths.slice(0, 3).map((path, index) => <img key={path} src={`${tmdbImageBase}${path}`} alt="" loading="lazy" style={{ "--stack-index": index } as CSSProperties} />)}
                  </span> : <span
                    className="group-mark topic-mark"
                    style={topic.icon_color ? { color: `#${topic.icon_color.toString(16).padStart(6, "0")}` } : undefined}
                  >
                    <MessageCircle size={19} />
                  </span>}
                  <span className="group-copy">
                    <strong>{topic.title}</strong>
                    <small>
                      {topic.hidden ? "Oculto" : topic.closed ? "Cerrado" : `${activeMode === "manage" ? topic.library_kind === "movie_collection" ? "Películas · " : topic.library_kind === "series" ? "Serie · " : "" : ""}${topic.video_count || 0} vídeos${topic.exported_count ? ` · ${topic.exported_count} STRM` : ""}${activeMode === "manage" && topic.unresolved_count ? ` · ${topic.unresolved_count} por revisar` : ""}`}
                    </small>
                  </span>
                  {topic.pinned && <Pin className="topic-pin" size={15} aria-label="Fijado" />}
                  {activeMode === "manage" && selectingTopics && <span className={`topic-check${selected ? " selected" : ""}`}>{selected ? <CheckSquare2 size={18} /> : <Square size={18} />}</span>}
                </>;
                return activeMode === "manage" && selectingTopics ? (
                  <button key={topic.id} type="button" className={`group-card topic-card selectable${artwork ? " has-artwork" : ""}${selected ? " selected" : ""}`} aria-pressed={selected} onClick={() => setSelectedTopicIds((current) => { const next = new Set(current); selected ? next.delete(topic.id) : next.add(topic.id); return next; })}>{content}</button>
                ) : activeMode === "manage" ? (
                  <Link key={topic.id} className={`group-card topic-card${artwork ? " has-artwork" : ""}`} to={`/groups/${groupId}/topics/${topic.id}/manage`}>{content}</Link>
                ) : (
                  <Link key={topic.id} className={`group-card topic-card${artwork ? " has-artwork" : ""}`} to={`/groups/${groupId}/topics/${topic.id}`}>{content}</Link>
                );
              })}
            </section>
          ) : (
            <section className="topics-empty">
              <div className="empty-icon"><MessageCircle size={22} /></div>
              <h2>{topics.length ? "No encontramos ese topic" : groupDetail?.forum ? "Este foro no tiene topics visibles" : "Este grupo no utiliza topics"}</h2>
              <p>{topics.length ? "Prueba con otro nombre." : "Puedes volver al inicio para elegir otro grupo."}</p>
              <Link className="back-link" to="/home"><ArrowLeft size={15} /> Volver a tus grupos</Link>
            </section>
          )
        ) : visibleGroups.length ? (
          <section className="groups-grid" aria-label="Grupos de Telegram">
            {visibleGroups.map((group) => (
              <Link key={group.id} className="group-card" to={activeMode === "manage" ? `/groups/${group.id}/manage` : `/groups/${group.id}`}>
                <span className="group-mark">
                  <FolderOpen size={19} />
                </span>
                <span className="group-copy">
                  <strong>{group.title}</strong>
                  <small>
                    {group.forum
                      ? "Foro con topics"
                      : group.username
                        ? `@${group.username}`
                        : "Grupo de Telegram"}
                  </small>
                </span>
              </Link>
            ))}
          </section>
        ) : (
          <section className="topics-empty">
            <div className="empty-icon">
              <FolderOpen size={22} />
            </div>
            <h2>
              {groups.length
                ? "No encontramos ese grupo"
                : "Buscando tus grupos"}
            </h2>
            <p>
              {groups.length
                ? "Prueba con otro nombre o usuario."
                : "Telegram está preparando la lista de grupos disponibles."}
            </p>
          </section>
        )}</>}
      </main>
      {logoutOpen && <div className="logout-backdrop" role="presentation" onMouseDown={() => !logoutBusy && setLogoutOpen(false)}>
        <section className="logout-dialog" role="dialog" aria-modal="true" aria-labelledby="logout-title" onMouseDown={(event) => event.stopPropagation()}>
          <span className="logout-dialog-icon"><LogOut size={20} /></span>
          <div><p className="eyebrow">CUENTA DE TELEGRAM</p><h2 id="logout-title">¿Cerrar esta sesión?</h2><p>Telegram solicitará de nuevo tu teléfono, el código de acceso y la contraseña 2FA si la utilizas. El catálogo, los STRM, los ajustes y los trabajos pendientes se conservarán.</p></div>
          {logoutError && <p className="logout-error" role="alert">{logoutError}</p>}
          <footer><button type="button" className="logout-cancel" disabled={logoutBusy} onClick={() => setLogoutOpen(false)}>Cancelar</button><button type="button" className="logout-confirm" autoFocus disabled={logoutBusy} onClick={() => void logoutTelegram()}>{logoutBusy ? <LoaderCircle className="spin" size={15} /> : <LogOut size={15} />}{logoutBusy ? "Cerrando…" : "Cerrar sesión"}</button></footer>
        </section>
      </div>}
      {scrollIndicator.visible && (
        <span
          ref={scrollEdgeRef}
          className="dashboard-scroll-edge"
          aria-hidden="true"
          onPointerDown={handleTrackPointerDown}
          onPointerMove={handleTrackPointerMove}
          onPointerUp={finishScrollDrag}
          onPointerCancel={finishScrollDrag}
          onLostPointerCapture={() => {
            dragOffsetRef.current = undefined;
            setDraggingScroll(false);
          }}
        >
          <span
            className="dashboard-scroll-thumb"
            onPointerDown={handleThumbPointerDown}
            style={{
              height: `${scrollIndicator.height}px`,
              top: `${scrollIndicator.top}px`,
            }}
          />
        </span>
      )}
      </div>
      {resolutionTarget && (
        <div className="episode-resolution-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setResolutionTarget(undefined); }}>
          <section className="episode-resolution" role="dialog" aria-modal="true" aria-labelledby="episode-resolution-title">
            <header>
              <div><p className="eyebrow">RESOLUCIÓN MANUAL</p><h2 id="episode-resolution-title">Asignar episodio</h2></div>
              <button type="button" className="tmdb-close" aria-label="Cerrar" onClick={() => setResolutionTarget(undefined)}><X size={17} /></button>
            </header>
            <div className="resolution-source"><span className="message-poster"><FileVideo size={22} /></span><div><strong>{resolutionTarget.body || resolutionTarget.file_name}</strong><small>{resolutionTarget.file_name}</small></div></div>
            {resolutionInfo && <div className="resolution-series"><CheckCircle2 size={16} /><span><small>Serie confirmada en TMDB</small><strong>{resolutionInfo.series.title}</strong></span><b>#{resolutionInfo.series.tmdb_id}</b></div>}
            <div className="resolution-controls">
              <label><span>Temporada</span><input type="number" min="0" max="99" value={resolutionSeason} onChange={(event) => { setResolutionSeason(Number(event.target.value)); setResolutionEpisode(0); setEpisodeOptions([]); }} /></label>
              <button type="button" onClick={() => void loadSeasonEpisodes()} disabled={!resolutionInfo || resolutionState !== "idle"}>{resolutionState === "episodes" ? <LoaderCircle className="spin" size={15} /> : <ScanSearch size={15} />} Ver episodios</button>
            </div>
            {episodeOptions.length > 0 && <div className="episode-options" role="listbox" aria-label={`Episodios de la temporada ${resolutionSeason}`}>
              {episodeOptions.map((episode) => <button key={episode.number} type="button" className={resolutionEpisode === episode.number ? "selected" : ""} onClick={() => setResolutionEpisode(episode.number)}>
                <span>{episode.still_path ? <img src={`https://image.tmdb.org/t/p/w300${episode.still_path}`} alt="" loading="lazy" /> : <FileVideo size={18} />}</span>
                <div><strong>E{String(episode.number).padStart(2, "0")} · {episode.name || `Episodio ${episode.number}`}</strong><small>{episode.air_date || "Sin fecha publicada"}</small></div>
                {resolutionEpisode === episode.number && <CheckCircle2 size={17} />}
              </button>)}
            </div>}
            {resolutionInfo && resolutionEpisode > 0 && <div className="resolution-preview"><small>Vista previa</small><strong>{resolutionInfo.series.title} · S{String(resolutionSeason).padStart(2, "0")}E{String(resolutionEpisode).padStart(2, "0")}</strong></div>}
            {resolutionError && <p className="resolution-error" role="alert">{resolutionError}</p>}
            <footer><button type="button" className="resolution-cancel" onClick={() => setResolutionTarget(undefined)}>Cancelar</button><button type="button" className="resolution-save" disabled={!resolutionInfo || resolutionEpisode < 1 || resolutionState !== "idle"} onClick={() => void saveEpisodeResolution()}>{resolutionState === "saving" ? <LoaderCircle className="spin" size={15} /> : <Check size={15} />} Confirmar y generar</button></footer>
          </section>
        </div>
      )}
      {topicMatchOpen && (
        <div className="episode-resolution-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setTopicMatchOpen(false); }}>
          <section className="episode-resolution topic-match-dialog" role="dialog" aria-modal="true" aria-labelledby="topic-match-title">
            <header><div><p className="eyebrow">COINCIDENCIA MANUAL</p><h2 id="topic-match-title">Buscar serie en TMDB</h2></div><button type="button" className="tmdb-close" aria-label="Cerrar" onClick={() => setTopicMatchOpen(false)}><X size={17} /></button></header>
            <p className="tmdb-hint">Elige la ficha correcta para recuperar la portada, el año y los datos usados al generar la biblioteca.</p>
            <div className="tmdb-search-row"><label className="group-search"><Search size={16} /><input value={topicMatchQuery} onChange={(event) => setTopicMatchQuery(event.target.value)} onKeyDown={(event) => { if (event.key === "Enter") { event.preventDefault(); void searchTopicMatch(); } }} autoFocus aria-label="Título de la serie" /></label><button type="button" disabled={topicMatchState !== "idle"} onClick={() => void searchTopicMatch()}>{topicMatchState === "searching" ? <LoaderCircle className="spin" size={15} /> : <Search size={15} />} Buscar</button></div>
            {topicMatchError && <p className="tmdb-error" role="alert">{topicMatchError}</p>}
            <div className="tmdb-results">{topicMatchResults.map((candidate) => <article className="tmdb-result" key={candidate.id}>{candidate.poster_path ? <img src={`${tmdbImageBase}${candidate.poster_path}`} alt="" /> : <span className="tmdb-poster-empty"><FileVideo size={20} /></span>}<div><strong>{candidate.title}</strong><small>{candidate.release_date?.slice(0, 4) || "Año no disponible"}{candidate.original_title && candidate.original_title !== candidate.title ? ` · ${candidate.original_title}` : ""}</small><p>{candidate.overview || "Sin descripción disponible."}</p></div><button type="button" disabled={topicMatchState !== "idle"} onClick={() => void confirmTopicMatch(candidate)}>{topicMatchState === "saving" ? <LoaderCircle className="spin" size={14} /> : <Check size={14} />} Asociar</button></article>)}</div>
          </section>
        </div>
      )}
      {movieMatchTarget && (
        <div className="episode-resolution-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setMovieMatchTarget(undefined); }}>
          <section className="episode-resolution topic-match-dialog" role="dialog" aria-modal="true" aria-labelledby="movie-match-title">
            <header><div><p className="eyebrow">REVISIÓN DE PELÍCULA</p><h2 id="movie-match-title">Confirmar coincidencia en TMDB</h2></div><button type="button" className="tmdb-close" aria-label="Cerrar" onClick={() => setMovieMatchTarget(undefined)}><X size={17} /></button></header>
            <div className="resolution-source"><span className="message-poster"><FileVideo size={22} /></span><div><strong>{movieMatchTarget.body || movieMatchTarget.file_name}</strong><small>{movieMatchTarget.file_name}</small></div></div>
            <p className="tmdb-hint">Confirma la ficha correcta. El título en España y su año formarán tanto la carpeta como el archivo STRM.</p>
            <div className="tmdb-search-row"><label className="group-search"><Search size={16} /><input value={movieMatchQuery} onChange={(event) => setMovieMatchQuery(event.target.value)} onKeyDown={(event) => { if (event.key === "Enter") { event.preventDefault(); void searchMovieMatch(); } }} autoFocus aria-label="Título de la película" /></label><button type="button" disabled={movieMatchState !== "idle"} onClick={() => void searchMovieMatch()}>{movieMatchState === "searching" ? <LoaderCircle className="spin" size={15} /> : <Search size={15} />} Buscar</button></div>
            {movieMatchError && <p className="tmdb-error" role="alert">{movieMatchError}</p>}
            <div className="tmdb-results">{movieMatchResults.map((candidate) => <article className="tmdb-result" key={candidate.id}>{candidate.poster_path ? <img src={`${tmdbImageBase}${candidate.poster_path}`} alt="" /> : <span className="tmdb-poster-empty"><FileVideo size={20} /></span>}<div><strong>{candidate.title}</strong><small>{candidate.release_date?.slice(0, 4) || "Año no disponible"}{candidate.original_title && candidate.original_title !== candidate.title ? ` · ${candidate.original_title}` : ""}</small><p>{candidate.overview || "Sin descripción disponible."}</p></div><button type="button" disabled={movieMatchState !== "idle"} onClick={() => void confirmMovieMatch(candidate)}>{movieMatchState === "saving" ? <LoaderCircle className="spin" size={14} /> : <Check size={14} />} Asociar</button></article>)}</div>
          </section>
        </div>
      )}
    </div>
  );
}
