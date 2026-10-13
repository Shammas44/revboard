import { DatePipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { RouterLink } from '@angular/router';
import { ConfirmationService, MessageService } from 'primeng/api';
import { Button } from 'primeng/button';
import { Dialog } from 'primeng/dialog';
import { InputText } from 'primeng/inputtext';
import { ProgressBar } from 'primeng/progressbar';
import { TableModule } from 'primeng/table';
import { Tag } from 'primeng/tag';
import { ToggleSwitch } from 'primeng/toggleswitch';
import { Tooltip } from 'primeng/tooltip';
import { forkJoin, map, of, switchMap } from 'rxjs';
import { ApiService, errorMessage } from '../../core/api.service';
import { ClearButton } from '../../core/clear-button';
import { Project, Session } from '../../core/models';

interface ProjectGroup {
  project: Project;
  sessions: Session[];
}

/** Lists every project with its review sessions. */
@Component({
  selector: 'grb-dashboard',
  imports: [RouterLink, FormsModule, DatePipe, Button, ClearButton, Dialog, InputText, ProgressBar, TableModule, Tag, ToggleSwitch, Tooltip],
  templateUrl: './dashboard.html',
  styleUrl: './dashboard.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class Dashboard {
  private readonly api = inject(ApiService);
  private readonly confirm = inject(ConfirmationService);
  private readonly toast = inject(MessageService);

  readonly groups = signal<ProjectGroup[]>([]);
  readonly loading = signal(true);
  readonly error = signal<string | null>(null);
  readonly showArchived = signal(false);
  readonly empty = computed(() => !this.loading() && this.groups().length === 0);
  /** Session being renamed and the edited name. */
  readonly renaming = signal<Session | null>(null);
  protected nameDraft = '';

  constructor() {
    this.reload();
  }

  reload(): void {
    this.loading.set(true);
    this.api
      .listProjects()
      .pipe(
        switchMap((projects) =>
          projects.length === 0
            ? of([])
            : forkJoin(
                projects.map((project) =>
                  this.api.listSessions(project.id, this.showArchived()).pipe(map((sessions) => ({ project, sessions }))),
                ),
              ),
        ),
      )
      .subscribe({
        next: (groups) => {
          this.groups.set(groups);
          this.error.set(null);
          this.loading.set(false);
        },
        error: (err) => {
          this.error.set(errorMessage(err));
          this.loading.set(false);
        },
      });
  }

  setShowArchived(value: boolean): void {
    this.showArchived.set(value);
    this.reload();
  }

  percent(s: Session): number {
    const p = s.progress;
    return p.totalHunks > 0
      ? Math.floor((p.reviewedHunks * 100) / p.totalHunks)
      : p.totalFiles > 0
        ? Math.floor((p.reviewedFiles * 100) / p.totalFiles)
        : 0;
  }

  command(s: Session): string {
    return 'git ' + s.gitArgs.map((a) => (/[\s'"*?()[\]$`\\]/.test(a) || a === '' ? `'${a}'` : a)).join(' ');
  }

  setArchived(s: Session, archived: boolean): void {
    this.api.patchSession(s.id, { state: archived ? 'archived' : 'active' }).subscribe({
      next: () => this.reload(),
      error: (err) => this.toast.add({ severity: 'error', summary: 'Error', detail: errorMessage(err) }),
    });
  }

  startRename(s: Session): void {
    this.nameDraft = s.name;
    this.renaming.set(s);
  }

  saveRename(): void {
    const s = this.renaming();
    const name = this.nameDraft.trim();
    if (!s || !name) return;
    this.api.patchSession(s.id, { name }).subscribe({
      next: () => {
        this.renaming.set(null);
        this.reload();
      },
      error: (err) => this.toast.add({ severity: 'error', summary: 'Error', detail: errorMessage(err) }),
    });
  }

  remove(s: Session): void {
    this.confirm.confirm({
      header: 'Delete review session',
      message: `Delete "${s.name}" with all its statuses and notes? This cannot be undone.`,
      icon: 'pi pi-trash',
      acceptButtonProps: { label: 'Delete', severity: 'danger' },
      rejectButtonProps: { label: 'Cancel', severity: 'secondary', text: true },
      accept: () =>
        this.api.deleteSession(s.id).subscribe({
          next: () => this.reload(),
          error: (err) => this.toast.add({ severity: 'error', summary: 'Error', detail: errorMessage(err) }),
        }),
    });
  }
}
