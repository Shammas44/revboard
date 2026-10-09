import { ChangeDetectionStrategy, Component } from '@angular/core';
import { RouterOutlet } from '@angular/router';
import { ConfirmDialog } from 'primeng/confirmdialog';
import { Toast } from 'primeng/toast';

@Component({
  selector: 'app-root',
  imports: [RouterOutlet, Toast, ConfirmDialog],
  template: `
    <router-outlet />
    <p-toast position="bottom-right" />
    <p-confirmdialog />
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class App {}
