import { Routes } from '@angular/router';

export const routes: Routes = [
  {
    path: '',
    title: 'revboard — Reviews',
    loadComponent: () => import('./features/dashboard/dashboard').then((m) => m.Dashboard),
  },
  {
    path: 'sessions/:id',
    title: 'revboard — Review',
    loadComponent: () => import('./features/review/review-page').then((m) => m.ReviewPage),
  },
  { path: '**', redirectTo: '' },
];
