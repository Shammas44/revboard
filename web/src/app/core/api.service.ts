import { HttpClient, HttpErrorResponse, HttpInterceptorFn } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable } from 'rxjs';
import {
  Comment,
  CommitList,
  NoteContext,
  NoteCreated,
  FileDetail,
  FileSummary,
  NoteAnchor,
  Project,
  ReviewStatus,
  Session,
  SessionDetail,
  StatusChange,
  UiState,
} from './models';

/** Header required by the Go server on every API call (CSRF protection). */
export const clientHeaderInterceptor: HttpInterceptorFn = (req, next) =>
  next(req.url.startsWith('/api/') ? req.clone({ setHeaders: { 'X-GRB-Client': 'web' } }) : req);

/** Extracts a readable message from an HTTP error. */
export function errorMessage(err: unknown): string {
  if (err instanceof HttpErrorResponse) {
    return err.error?.error ?? (err.status === 0 ? 'The revboard server is not reachable. Is "revboard" still running?' : err.message);
  }
  return err instanceof Error ? err.message : String(err);
}

/** Typed client of the revboard REST API. */
@Injectable({ providedIn: 'root' })
export class ApiService {
  private readonly http = inject(HttpClient);

  listProjects(): Observable<Project[]> {
    return this.http.get<Project[]>('/api/projects');
  }

  listSessions(projectId: number, archived = false): Observable<Session[]> {
    return this.http.get<Session[]>(`/api/projects/${projectId}/sessions`, { params: archived ? { archived: '1' } : {} });
  }

  getSession(id: number): Observable<SessionDetail> {
    return this.http.get<SessionDetail>(`/api/sessions/${id}`);
  }

  patchSession(
    id: number,
    patch: { name?: string; state?: 'active' | 'archived'; exclusions?: string[]; uiState?: UiState },
  ): Observable<Session> {
    return this.http.patch<Session>(`/api/sessions/${id}`, patch);
  }

  deleteSession(id: number): Observable<void> {
    return this.http.delete<void>(`/api/sessions/${id}`);
  }

  refreshSession(id: number): Observable<SessionDetail> {
    return this.http.post<SessionDetail>(`/api/sessions/${id}/refresh`, null);
  }

  listCommits(sessionId: number, path?: string): Observable<CommitList> {
    return this.http.get<CommitList>(`/api/sessions/${sessionId}/commits`, { params: path ? { path } : {} });
  }

  setFilesStatus(sessionId: number, fileIds: number[], status: ReviewStatus, deleteNotes = false): Observable<StatusChange> {
    return this.http.post<StatusChange>(`/api/sessions/${sessionId}/status`, { fileIds, status, deleteNotes });
  }

  /** Markdown export of the notes marked for export (all: every note). */
  exportNotes(sessionId: number, all = false): Observable<string> {
    return this.http.get(`/api/sessions/${sessionId}/notes/export`, { params: all ? { all: '1' } : {}, responseType: 'text' });
  }

  /** Marks or unmarks every note of a session for export. */
  setAllNotesExport(sessionId: number, exported: boolean): Observable<void> {
    return this.http.put<void>(`/api/sessions/${sessionId}/notes/export`, { export: exported });
  }

  getFile(id: number): Observable<FileDetail> {
    return this.http.get<FileDetail>(`/api/files/${id}`);
  }

  setFileStatus(id: number, status: ReviewStatus, deleteNotes = false): Observable<StatusChange> {
    return this.http.put<StatusChange>(`/api/files/${id}/status`, { status, deleteNotes });
  }

  setPinned(id: number, pinned: boolean): Observable<FileSummary> {
    return this.http.put<FileSummary>(`/api/files/${id}/pin`, { pinned });
  }

  setHunkStatus(id: number, status: ReviewStatus, deleteNotes = false): Observable<StatusChange> {
    return this.http.put<StatusChange>(`/api/hunks/${id}/status`, { status, deleteNotes });
  }

  listComments(sessionId: number): Observable<Comment[]> {
    return this.http.get<Comment[]>(`/api/sessions/${sessionId}/comments`);
  }

  listNotes(sessionId: number): Observable<NoteContext[]> {
    return this.http.get<NoteContext[]>(`/api/sessions/${sessionId}/notes`);
  }

  createComment(sessionId: number, anchor: NoteAnchor, body: string): Observable<NoteCreated> {
    return this.http.post<NoteCreated>(`/api/sessions/${sessionId}/comments`, { ...anchor, body });
  }

  updateComment(id: number, patch: { body?: string; export?: boolean }): Observable<Comment> {
    return this.http.patch<Comment>(`/api/comments/${id}`, patch);
  }

  deleteComment(id: number): Observable<void> {
    return this.http.delete<void>(`/api/comments/${id}`);
  }
}
