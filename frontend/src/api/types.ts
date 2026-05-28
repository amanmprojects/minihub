export type Repository = {
  name: string;
  description: string;
  defaultBranch: string;
  protectedBranches: string[];
  cloneUrl: string;
  createdAt: string;
  updatedAt: string;
};

export type Branch = {
  name: string;
  commit: string;
  updatedAt: string;
  default: boolean;
  protected: boolean;
};

export type TreeEntry = {
  name: string;
  path: string;
  type: "tree" | "blob";
};

export type Commit = {
  hash: string;
  author: string;
  authorEmail: string;
  date: string;
  subject: string;
};

export type CommitDetail = Commit & {
  body: string;
  diff: string;
};

export type Session = {
  token: string;
};

export type PullRequest = {
  id: number;
  number: number;
  title: string;
  body?: string;
  sourceBranch: string;
  targetBranch: string;
  status: string;
  mergeCommitSha?: string;
};

export type Review = {
  id: number;
  state: string;
  body?: string;
};

export type Comment = {
  id: number;
  body?: string;
  filePath?: string;
  lineNumber?: number;
};

export type Issue = {
  id: number;
  number: number;
  title: string;
  body?: string;
  status: string;
};

export type Release = {
  id: number;
  tagName: string;
  title: string;
  notes?: string;
};

export type Webhook = {
  id: number;
  url: string;
  events: string;
  active: boolean;
};

export type CIRun = {
  id: number;
  commitSha?: string;
  branch?: string;
  status: string;
  provider?: string;
  logUrl?: string;
};

export type User = {
  id: number;
  username: string;
  displayName?: string;
  email: string;
};

export type Organization = {
  id: number;
  name: string;
  displayName?: string;
};

export type Permission = {
  userId: number;
  username: string;
  role: string;
};
